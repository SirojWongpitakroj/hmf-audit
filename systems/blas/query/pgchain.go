package query

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"blas/chain"
	"blas/optimize"
	"github.com/jackc/pgx/v5"
)

// PostgresChain is a storage-backed ChainView: the full node's block-level index (one row per
// segment) and transaction-level index (one row per block) live in PostgreSQL, and a request reads
// and decodes only the segments and blocks it needs. Nothing is kept between requests except the
// chain geometry (segment size, block and segment counts) and the on-chain keyword registry, which
// are a few numbers and a short word list. Rows are gob encodings of chain.Segment and chain.Block,
// the same encodings the persistent full-node snapshot (chain.Chain.GobEncode) is made of.
//
// The connections are used one request phase at a time (Stage 1, then Stage 2, then the storage
// provider), never by two readers at once, so a request may share its connections with the
// PostgresSP that serves the entry files.
type PostgresChain struct {
	ctx      context.Context
	conns    []*pgx.Conn
	segSize  uint64
	blocks   uint64
	segments uint64
	registry map[uint64][]string

	reads atomic.Int64 // database round trips since BeginRequest
	bytes atomic.Int64 // payload bytes read since BeginRequest

	mu       sync.Mutex // guards the per-request memos
	segMemo  map[uint64]*chain.Segment
	blockMem map[uint64]*chain.Block
}

var _ ChainView = (*PostgresChain)(nil)

const chainSchema = `
CREATE SCHEMA IF NOT EXISTS blas;
CREATE TABLE IF NOT EXISTS blas.chain_segments (
    seg             BIGINT PRIMARY KEY,
    anchor_ts_min   BIGINT NOT NULL,
    anchor_ts_max   BIGINT NOT NULL,
    anchor_past_max BIGINT NOT NULL,
    payload         BYTEA  NOT NULL
);
CREATE TABLE IF NOT EXISTS blas.chain_blocks (
    height  BIGINT PRIMARY KEY,
    payload BYTEA  NOT NULL
);
CREATE TABLE IF NOT EXISTS blas.chain_meta (
    key     TEXT PRIMARY KEY,
    payload BYTEA NOT NULL
);`

// chainGeometry is the one chain_meta row "geometry".
type chainGeometry struct {
	SegmentSize uint64
	Blocks      uint64
	Segments    uint64
	Registry    map[uint64][]string
}

func encodeGob(value any) ([]byte, error) {
	var out bytes.Buffer
	if err := gob.NewEncoder(&out).Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// SaveChain writes the chain's segments, blocks and geometry to PostgreSQL, replacing what the
// three chain tables held. It returns the payload bytes written. This is setup, outside every timer.
func SaveChain(ctx context.Context, conn *pgx.Conn, c *chain.Chain) (int64, error) {
	if _, err := conn.Exec(ctx, chainSchema); err != nil {
		return 0, fmt.Errorf("create chain tables: %w", err)
	}
	if _, err := conn.Exec(ctx, "TRUNCATE blas.chain_segments, blas.chain_blocks, blas.chain_meta"); err != nil {
		return 0, err
	}
	blocks := c.Blocks()
	segments := c.Segments()
	var total int64

	next := 0
	rows := pgx.CopyFromFunc(func() ([]any, error) {
		if next == len(segments) {
			return nil, nil
		}
		index := next
		next++
		payload, err := encodeGob(segments[index])
		if err != nil {
			return nil, fmt.Errorf("encode segment %d: %w", index, err)
		}
		anchor := optimize.AnchorHeight(uint64(index), c.SegmentSize(), len(blocks))
		header := blocks[anchor].Header
		total += int64(len(payload))
		return []any{int64(index), header.BlockTsMin, header.BlockTsMax, header.PastTsMax, payload}, nil
	})
	if _, err := conn.CopyFrom(ctx, pgx.Identifier{"blas", "chain_segments"},
		[]string{"seg", "anchor_ts_min", "anchor_ts_max", "anchor_past_max", "payload"}, rows); err != nil {
		return 0, fmt.Errorf("copy chain segments: %w", err)
	}

	next = 0
	rows = pgx.CopyFromFunc(func() ([]any, error) {
		if next == len(blocks) {
			return nil, nil
		}
		height := next
		next++
		payload, err := encodeGob(blocks[height])
		if err != nil {
			return nil, fmt.Errorf("encode block %d: %w", height, err)
		}
		total += int64(len(payload))
		return []any{int64(height), payload}, nil
	})
	if _, err := conn.CopyFrom(ctx, pgx.Identifier{"blas", "chain_blocks"},
		[]string{"height", "payload"}, rows); err != nil {
		return 0, fmt.Errorf("copy chain blocks: %w", err)
	}

	meta, err := encodeGob(chainGeometry{
		SegmentSize: c.SegmentSize(), Blocks: uint64(len(blocks)),
		Segments: uint64(len(segments)), Registry: c.KeywordRegistry(),
	})
	if err != nil {
		return 0, err
	}
	if _, err := conn.Exec(ctx, "INSERT INTO blas.chain_meta (key, payload) VALUES ('geometry', $1)", meta); err != nil {
		return 0, err
	}
	return total + int64(len(meta)), nil
}

// OpenPostgresChain reads the chain geometry and returns a view over the stored chain. conns are
// the connections the view may read with (one per concurrent reader); it spreads a request's reads
// over all of them.
func OpenPostgresChain(ctx context.Context, conns []*pgx.Conn) (*PostgresChain, error) {
	if len(conns) == 0 {
		return nil, errors.New("query: PostgresChain needs at least one connection")
	}
	var payload []byte
	if err := conns[0].QueryRow(ctx, "SELECT payload FROM blas.chain_meta WHERE key = 'geometry'").Scan(&payload); err != nil {
		return nil, fmt.Errorf("read stored chain geometry: %w", err)
	}
	var geometry chainGeometry
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&geometry); err != nil {
		return nil, fmt.Errorf("decode stored chain geometry: %w", err)
	}
	if geometry.SegmentSize == 0 || geometry.Blocks == 0 {
		return nil, errors.New("query: stored chain is empty")
	}
	view := &PostgresChain{ctx: ctx, conns: conns, segSize: geometry.SegmentSize, blocks: geometry.Blocks,
		segments: geometry.Segments, registry: geometry.Registry}
	view.BeginRequest()
	return view, nil
}

func (v *PostgresChain) SegmentSize() uint64 { return v.segSize }

func (v *PostgresChain) KeywordRegistry() map[uint64][]string {
	out := make(map[uint64][]string, len(v.registry))
	for lid, universe := range v.registry {
		out[lid] = append([]string{}, universe...)
	}
	return out
}

// BeginRequest drops the segments and blocks kept for the previous request and resets the read counters.
func (v *PostgresChain) BeginRequest() {
	v.mu.Lock()
	v.segMemo = make(map[uint64]*chain.Segment)
	v.blockMem = make(map[uint64]*chain.Block)
	v.mu.Unlock()
	v.reads.Store(0)
	v.bytes.Store(0)
}

// Stats reports the database round trips and payload bytes read since BeginRequest.
func (v *PostgresChain) Stats() (reads, payloadBytes int64) { return v.reads.Load(), v.bytes.Load() }

// AnchorRow is the stored anchor header times of one segment.
type AnchorRow struct {
	Seg   uint64
	Times optimize.HeaderTimes
}

// BackwardScan is optimize.IntersectingSegments (the R-OPT-004..007 backward scan) over anchor
// headers fetched newest first, one page at a time: fetch(upper) returns the anchors of segments
// upper, upper-1, ... (descending) and the scan stops where optimize.IntersectingSegments stops.
func BackwardScan(segments uint64, hasRange bool, ts, te int64,
	fetch func(upper int64) ([]AnchorRow, error)) ([]uint64, error) {

	if !hasRange {
		all := make([]uint64, segments)
		for i := range all {
			all[i] = uint64(i)
		}
		return all, nil
	}
	var selected []uint64
	next := int64(segments) - 1
	for next >= 0 {
		rows, err := fetch(next)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("query: no stored anchor header at or below segment %d", next)
		}
		for _, row := range rows {
			if int64(row.Seg) != next {
				return nil, fmt.Errorf("query: stored anchor headers skip segment %d", next)
			}
			if row.Times.BlockTsMin <= te && row.Times.BlockTsMax >= ts {
				selected = append(selected, row.Seg)
			}
			if row.Times.PastTsMax < ts {
				next = -1
				break
			}
			next--
		}
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected, nil
}

// IntersectingSegments reads the stored anchor headers newest first, 64 segments per round trip.
func (v *PostgresChain) IntersectingSegments(hasRange bool, ts, te int64) ([]uint64, error) {
	const page = 64
	return BackwardScan(v.segments, hasRange, ts, te, func(upper int64) ([]AnchorRow, error) {
		rows, err := v.conns[0].Query(v.ctx, `
			SELECT seg, anchor_ts_min, anchor_ts_max, anchor_past_max FROM blas.chain_segments
			WHERE seg <= $1 AND seg > $2 ORDER BY seg DESC`, upper, upper-page)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		v.reads.Add(1)
		var out []AnchorRow
		for rows.Next() {
			var seg int64
			var times optimize.HeaderTimes
			if err := rows.Scan(&seg, &times.BlockTsMin, &times.BlockTsMax, &times.PastTsMax); err != nil {
				return nil, err
			}
			v.bytes.Add(32)
			out = append(out, AnchorRow{Seg: uint64(seg), Times: times})
		}
		return out, rows.Err()
	})
}

func (v *PostgresChain) Segments(indexes []uint64) ([]*chain.Segment, error) {
	for _, index := range indexes {
		if index >= v.segments {
			return nil, fmt.Errorf("query: segment index %d out of range", index)
		}
	}
	v.mu.Lock()
	var missing []uint64
	seen := map[uint64]bool{}
	for _, index := range indexes {
		if _, ok := v.segMemo[index]; !ok && !seen[index] {
			seen[index] = true
			missing = append(missing, index)
		}
	}
	v.mu.Unlock()
	loaded, err := fetchDecoded(v, "blas.chain_segments", "seg", missing, func(payload []byte) (*chain.Segment, error) {
		var segment chain.Segment
		if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&segment); err != nil {
			return nil, err
		}
		return &segment, nil
	})
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for index, segment := range loaded {
		v.segMemo[index] = segment
	}
	out := make([]*chain.Segment, len(indexes))
	for i, index := range indexes {
		out[i] = v.segMemo[index]
	}
	return out, nil
}

func (v *PostgresChain) Blocks(heights []uint64) ([]*chain.Block, error) {
	for _, height := range heights {
		if height >= v.blocks {
			return nil, fmt.Errorf("query: candidate height %d out of range", height)
		}
	}
	v.mu.Lock()
	var missing []uint64
	seen := map[uint64]bool{}
	for _, height := range heights {
		if _, ok := v.blockMem[height]; !ok && !seen[height] {
			seen[height] = true
			missing = append(missing, height)
		}
	}
	v.mu.Unlock()
	loaded, err := fetchDecoded(v, "blas.chain_blocks", "height", missing, func(payload []byte) (*chain.Block, error) {
		var block chain.Block
		if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&block); err != nil {
			return nil, err
		}
		return &block, nil
	})
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for height, block := range loaded {
		v.blockMem[height] = block
	}
	out := make([]*chain.Block, len(heights))
	for i, height := range heights {
		out[i] = v.blockMem[height]
	}
	return out, nil
}

// fetchDecoded reads the rows with the given keys, one query per connection over contiguous slices
// of the (sorted-as-given) keys, and decodes each payload on the goroutine that read it.
func fetchDecoded[T any](v *PostgresChain, table, keyColumn string, keys []uint64,
	decode func([]byte) (*T, error)) (map[uint64]*T, error) {

	out := make(map[uint64]*T, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	workers := min(len(v.conns), len(keys))
	per := (len(keys) + workers - 1) / workers
	query := fmt.Sprintf("SELECT %s, payload FROM %s WHERE %s = ANY($1)", keyColumn, table, keyColumn)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	for w := 0; w < workers; w++ {
		lo, hi := w*per, min((w+1)*per, len(keys))
		if lo >= hi {
			break
		}
		wg.Add(1)
		go func(conn *pgx.Conn, chunk []uint64) {
			defer wg.Done()
			ids := make([]int64, len(chunk))
			for i, key := range chunk {
				ids[i] = int64(key)
			}
			rows, err := conn.Query(v.ctx, query, ids)
			if err != nil {
				fail(err)
				return
			}
			defer rows.Close()
			v.reads.Add(1)
			decoded := make(map[uint64]*T, len(chunk))
			for rows.Next() {
				var key int64
				var payload []byte
				if err := rows.Scan(&key, &payload); err != nil {
					fail(err)
					return
				}
				v.bytes.Add(int64(len(payload)))
				value, err := decode(payload)
				if err != nil {
					fail(fmt.Errorf("decode %s %d: %w", table, key, err))
					return
				}
				decoded[uint64(key)] = value
			}
			if err := rows.Err(); err != nil {
				fail(err)
				return
			}
			mu.Lock()
			for key, value := range decoded {
				out[key] = value
			}
			mu.Unlock()
		}(v.conns[w], keys[lo:hi])
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	for _, key := range keys {
		if out[key] == nil {
			return nil, fmt.Errorf("query: %s %d is not stored", table, key)
		}
	}
	return out, nil
}
