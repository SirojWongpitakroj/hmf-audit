package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	contract "canonicalingestion"
	"github.com/jackc/pgx/v5"
)

const maxSampleRecords = 10000
const maxSampleBytes = 64 << 20

var publicationLimits = contract.Limits{MaxRecords: 1000, MaxBatchBytes: 8 << 20, MaxRecordBytes: 1 << 20}

type sampleInfo struct {
	Stream                                         string `json:"stream"`
	Fingerprint                                    string `json:"fingerprint"`
	OriginalFirst, OriginalLast, SnapshotCommitted uint64
	Records, PayloadBytes, AccountedInputBytes     int
	NormalizedPrefixSHA256                         string `json:"normalized_prefix_sha256"`
	OriginalPrefixSHA256                           string `json:"original_prefix_sha256"`
}
type sample struct {
	info          sampleInfo
	entries       []contract.Entry
	batches       [][]contract.Entry
	batchPrefixes [][32]byte
}

func prefixHash(previous [32]byte, e contract.Entry) [32]byte {
	h := sha256.New()
	h.Write(previous[:])
	var ordinal [8]byte
	binary.BigEndian.PutUint64(ordinal[:], e.Ordinal)
	h.Write(ordinal[:])
	h.Write([]byte(e.SourceID))
	payload := sha256.Sum256(e.Record)
	h.Write(payload[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func loadSample(ctx context.Context, source *pgx.Conn, stream string, after int64, count int) (sample, error) {
	var out sample
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var committed int64
	if err = tx.QueryRow(ctx, `SELECT fingerprint,committed FROM canonical_ingestion.streams WHERE stream_id=$1`, stream).Scan(&out.info.Fingerprint, &committed); err != nil {
		return out, err
	}
	if committed < int64(count) {
		return out, fmt.Errorf("source needs at least %d already-published records", count)
	}
	if after < 0 {
		after = committed - int64(count)
	}
	if after > math.MaxInt64-int64(count) || after+int64(count) > committed {
		return out, errors.New("sample is outside the committed canonical prefix")
	}
	out.info.Stream = stream
	out.info.OriginalFirst = uint64(after + 1)
	out.info.OriginalLast = uint64(after + int64(count))
	out.info.SnapshotCommitted = uint64(committed)
	var originalPrefix [32]byte
	if after > 0 {
		var b []byte
		if err = tx.QueryRow(ctx, `SELECT prefix_hash FROM canonical_ingestion.entries WHERE stream_id=$1 AND ordinal=$2`, stream, after).Scan(&b); err != nil {
			return out, err
		}
		if len(b) != 32 {
			return out, errors.New("invalid source prefix length")
		}
		copy(originalPrefix[:], b)
	}
	var normalized [32]byte
	for next := after; next < after+int64(count); {
		rows, err := tx.Query(ctx, `SELECT ordinal,source_id,octet_length(payload),CASE WHEN octet_length(payload)<=$4 THEN payload ELSE NULL END,payload_hash,prefix_hash FROM canonical_ingestion.entries WHERE stream_id=$1 AND ordinal>$2 AND ordinal<=$3 ORDER BY ordinal LIMIT 1000`, stream, next, after+int64(count), publicationLimits.MaxRecordBytes)
		if err != nil {
			return out, err
		}
		read := 0
		for rows.Next() {
			var e contract.Entry
			var n int64
			var size int
			var payloadHash, storedPrefix []byte
			if err = rows.Scan(&n, &e.SourceID, &size, &e.Record, &payloadHash, &storedPrefix); err != nil {
				rows.Close()
				return out, err
			}
			if size > publicationLimits.MaxRecordBytes || len(e.Record) != size || n != next+1 {
				rows.Close()
				return out, errors.New("oversized payload or missing source ordinal")
			}
			e.Ordinal = uint64(n)
			if err = e.Validate(publicationLimits.MaxRecordBytes); err != nil {
				rows.Close()
				return out, fmt.Errorf("source record validation at %d: %w", n, err)
			}
			ph := sha256.Sum256(e.Record)
			originalPrefix = prefixHash(originalPrefix, e)
			if !bytes.Equal(payloadHash, ph[:]) || !bytes.Equal(storedPrefix, originalPrefix[:]) {
				rows.Close()
				return out, fmt.Errorf("source payload/prefix hash differs at %d", n)
			}
			// Preserve source identity and exact payload. Renumber only the private
			// benchmark copy, so each fresh trial starts at prefix zero.
			e.Ordinal = uint64(len(out.entries) + 1)
			normalized = prefixHash(normalized, e)
			out.info.PayloadBytes += size
			out.info.AccountedInputBytes += size + len(e.SourceID) + 256
			if out.info.AccountedInputBytes > maxSampleBytes {
				rows.Close()
				return out, errors.New("sample exceeds 64 MiB retained input budget")
			}
			e.Record = append([]byte(nil), e.Record...) // detach from pgx row buffers
			out.entries = append(out.entries, e)
			next = n
			read++
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
		if read == 0 {
			return out, errors.New("missing source sample tail")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.info.Records = len(out.entries)
	out.info.NormalizedPrefixSHA256 = hex.EncodeToString(normalized[:])
	out.info.OriginalPrefixSHA256 = hex.EncodeToString(originalPrefix[:])
	for start := 0; start < len(out.entries); {
		end, total := start, 0
		for end < len(out.entries) && end-start < publicationLimits.MaxRecords {
			e := out.entries[end]
			cost := len(e.Record) + len(e.SourceID) + 8
			if total+cost > publicationLimits.MaxBatchBytes {
				break
			}
			total += cost
			end++
		}
		if end == start {
			return out, errors.New("one record exceeds publication byte budget")
		}
		out.batches = append(out.batches, out.entries[start:end])
		start = end
	}
	var expected [32]byte
	for _, batch := range out.batches {
		for _, e := range batch {
			expected = prefixHash(expected, e)
		}
		out.batchPrefixes = append(out.batchPrefixes, expected)
	}
	return out, nil
}

func validateDurability(ctx context.Context, conn *pgx.Conn) (map[string]string, error) {
	out := map[string]string{}
	for _, setting := range []string{"synchronous_commit", "fsync", "full_page_writes", "track_wal_io_timing", "server_version"} {
		var value string
		if err := conn.QueryRow(ctx, "SHOW "+setting).Scan(&value); err != nil {
			return nil, err
		}
		out[setting] = value
	}
	for _, key := range []string{"synchronous_commit", "fsync", "full_page_writes"} {
		if out[key] != "on" {
			return out, fmt.Errorf("required durability setting %s is %s; benchmark will not change it", key, out[key])
		}
	}
	return out, nil
}

func cloneTables(ctx context.Context, conn *pgx.Conn, schema string) error {
	for _, table := range []string{"streams", "entries"} {
		var kind, persistence string
		var rowSecurity bool
		var triggers int
		if err := conn.QueryRow(ctx, `SELECT c.relkind::text,c.relpersistence::text,c.relrowsecurity,(SELECT count(*) FROM pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='canonical_ingestion' AND c.relname=$1`, table).Scan(&kind, &persistence, &rowSecurity, &triggers); err != nil {
			return err
		}
		if kind != "r" || persistence != "p" || rowSecurity || triggers != 0 {
			return fmt.Errorf("source %s has nonstandard persistence/partitioning/RLS/triggers; refusing an inequivalent clone", table)
		}
		if _, err := conn.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, table}.Sanitize()+" (LIKE "+pgx.Identifier{"canonical_ingestion", table}.Sanitize()+" INCLUDING ALL)"); err != nil {
			return err
		}
	}
	// LIKE copies columns, storage settings, checks and indexes, but not FKs.
	var foreignKeys int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE contype='f' AND conrelid IN ('canonical_ingestion.entries'::regclass,'canonical_ingestion.streams'::regclass)`).Scan(&foreignKeys); err != nil {
		return err
	}
	var expected bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint c WHERE c.contype='f' AND c.conrelid='canonical_ingestion.entries'::regclass AND c.confrelid='canonical_ingestion.streams'::regclass AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='stream_id')]::smallint[] AND c.confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.confrelid AND attname='stream_id')]::smallint[] AND c.confupdtype='a' AND c.confdeltype='a' AND c.confmatchtype='s' AND NOT c.condeferrable AND c.convalidated)`).Scan(&expected); err != nil {
		return err
	}
	if foreignKeys != 1 || !expected {
		return errors.New("source foreign keys differ from expected canonical schema; clone requires review")
	}
	_, err := conn.Exec(ctx, "ALTER TABLE "+pgx.Identifier{schema, "entries"}.Sanitize()+" ADD FOREIGN KEY(stream_id) REFERENCES "+pgx.Identifier{schema, "streams"}.Sanitize()+"(stream_id)")
	return err
}

func sourceTableStats(ctx context.Context, conn *pgx.Conn) ([]map[string]any, error) {
	rows, err := conn.Query(ctx, `SELECT c.relname,pg_total_relation_size(c.oid),pg_relation_size(c.oid),pg_indexes_size(c.oid),c.reltuples::bigint,COALESCE(s.n_live_tup,0),COALESCE(s.n_dead_tup,0),COALESCE(s.n_tup_ins,0),COALESCE(s.n_tup_upd,0),COALESCE(s.n_tup_hot_upd,0),s.last_autovacuum::text,s.last_autoanalyze::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_stat_user_tables s ON s.relid=c.oid WHERE n.nspname='canonical_ingestion' AND c.relname IN ('entries','streams') ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		keys := []string{"table", "total_bytes", "heap_bytes", "index_bytes", "estimated_rows", "live_tuples", "dead_tuples", "inserts", "updates", "hot_updates", "last_autovacuum", "last_autoanalyze"}
		m := map[string]any{}
		for i, key := range keys {
			m[key] = values[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
