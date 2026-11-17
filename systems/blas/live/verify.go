package live

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"blas/alis"
	"blas/chain"
	"github.com/jackc/pgx/v5"
)

// Verify rebuilds each persisted entry-level file and every block's
// transaction-level roots from PostgreSQL. The stored chain snapshot is the
// trusted blockchain emulator for this run.
func Verify(ctx context.Context, conn *pgx.Conn, runID string) (Result, error) {
	if conn == nil || runID == "" {
		return Result{}, errors.New("blas live: PostgreSQL connection and run ID required")
	}
	if err := migrate(ctx, conn); err != nil {
		return Result{}, err
	}
	var lid, records, files, blocks int64
	var fanout int
	var status string
	var snapshot []byte
	if err := conn.QueryRow(ctx, `SELECT lid,fanout,status,record_count,file_count,block_count,chain_snapshot FROM blas.live_runs WHERE run_id=$1`, runID).
		Scan(&lid, &fanout, &status, &records, &files, &blocks, &snapshot); err != nil {
		return Result{}, err
	}
	if (status != "complete" && status != "ingesting") || len(snapshot) == 0 {
		return Result{}, fmt.Errorf("blas live: run %q is %s", runID, status)
	}
	var ch chain.Chain
	if err := gob.NewDecoder(bytes.NewReader(snapshot)).Decode(&ch); err != nil {
		return Result{}, err
	}
	if len(ch.Blocks()) != int(blocks) || ch.Fanout() != fanout {
		return Result{}, errors.New("blas live: chain geometry/count mismatch")
	}
	gen := alis.NewLogGenerator(uint64(lid))
	var result Result
	var summaries []alis.BlockSummary
	for height, block := range ch.Blocks() {
		if uint64(height)%ch.SegmentSize() == 0 {
			summaries = nil
		}
		if block == nil || len(block.Transactions) == 0 || block.TxIndex == nil || block.TxIndex.OMT == nil || block.TxIndex.IIMT == nil || block.TxIndex.RIMBTs["ts_min"] == nil || block.TxIndex.RIMBTs["ts_max"] == nil {
			return result, fmt.Errorf("blas live: block %d is malformed", height)
		}
		if block.Header.BlockHeight != uint64(height) || block.Header.TxNum != uint64(len(block.Transactions)) {
			return result, fmt.Errorf("blas live: block %d header mismatch", height)
		}
		if height > 0 && block.Header.PrevBlockHash != chain.HashBlockHeader(ch.Blocks()[height-1].Header) {
			return result, fmt.Errorf("blas live: block %d previous hash mismatch", height)
		}
		metas := make([]alis.FileMetadata, 0, len(block.Transactions))
		for _, tx := range block.Transactions {
			if tx.LID != uint64(lid) || tx.FID != result.Files {
				return result, fmt.Errorf("blas live: unexpected file ID %d", tx.FID)
			}
			var payload []byte
			if err := conn.QueryRow(ctx, `SELECT payload FROM blas.entry_files WHERE lid=$1 AND fid=$2`, lid, int64(tx.FID)).Scan(&payload); err != nil {
				return result, err
			}
			var file alis.EntryFile
			if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&file); err != nil {
				return result, err
			}
			if file.LID != tx.LID || file.FID != tx.FID || len(file.Entries) == 0 {
				return result, fmt.Errorf("blas live: file %d identity mismatch", tx.FID)
			}
			if file.IIMT == nil || file.OMT == nil {
				return result, fmt.Errorf("blas live: file %d index missing", tx.FID)
			}
			builder, err := alis.NewEntryFileBuilder(gen, tx.FID, fanout)
			if err != nil {
				return result, err
			}
			var last int64
			for i, entry := range file.Entries {
				if entry.ESN != uint64(i) || (i > 0 && entry.TS < last) {
					return result, fmt.Errorf("blas live: file %d order mismatch", tx.FID)
				}
				var rec Observation
				if err := json.Unmarshal(entry.V, &rec); err != nil {
					return result, err
				}
				if err := rec.Validate(); err != nil {
					return result, err
				}
				if rec.Timestamp.UnixMilli() != entry.TS || !reflect.DeepEqual(rec.Keywords(), entry.EW) {
					return result, fmt.Errorf("blas live: file %d observation mismatch", tx.FID)
				}
				if _, err := builder.AddEntry(entry.TS, entry.EW, entry.V); err != nil {
					return result, err
				}
				last = entry.TS
				result.Records++
			}
			_, meta, err := builder.Close()
			if err != nil {
				return result, err
			}
			storedIIMTRoot, err := file.IIMT.Root()
			if err != nil {
				return result, err
			}
			storedOMTRoot, err := file.OMT.Root()
			if err != nil {
				return result, err
			}
			if storedIIMTRoot != meta.RHIIMT || storedOMTRoot != meta.RHOMT {
				return result, fmt.Errorf("blas live: file %d stored index mismatch", tx.FID)
			}
			if !reflect.DeepEqual(alis.NewTransaction(tx.TSN, meta), tx) {
				return result, fmt.Errorf("blas live: file %d root/metadata mismatch", tx.FID)
			}
			metas = append(metas, meta)
			result.Files++
		}
		_, index, err := alis.BuildTransactionLevel(fanout, metas)
		if err != nil {
			return result, err
		}
		omtRoot, err := index.OMT.Root()
		if err != nil {
			return result, err
		}
		fkRoot, err := index.IIMT.Root()
		if err != nil {
			return result, err
		}
		minRoot, err := index.RIMBTs["ts_min"].Root()
		if err != nil {
			return result, err
		}
		maxRoot, err := index.RIMBTs["ts_max"].Root()
		if err != nil {
			return result, err
		}
		rh := block.Header.RH
		if omtRoot != rh.TxOMTRoot || fkRoot != rh.TxFKRoot || minRoot != rh.TxTsMinRoot || maxRoot != rh.TxTsMaxRoot {
			return result, fmt.Errorf("blas live: block %d transaction roots mismatch", height)
		}
		storedTxOMT, err := block.TxIndex.OMT.Root()
		if err != nil {
			return result, err
		}
		storedTxFK, err := block.TxIndex.IIMT.Root()
		if err != nil {
			return result, err
		}
		storedTxMin, err := block.TxIndex.RIMBTs["ts_min"].Root()
		if err != nil {
			return result, err
		}
		storedTxMax, err := block.TxIndex.RIMBTs["ts_max"].Root()
		if err != nil {
			return result, err
		}
		if storedTxOMT != omtRoot || storedTxFK != fkRoot || storedTxMin != minRoot || storedTxMax != maxRoot {
			return result, fmt.Errorf("blas live: block %d stored transaction index mismatch", height)
		}
		keywords := map[string]bool{}
		minTS, maxTS := metas[0].TsMin, metas[0].TsMax
		for _, meta := range metas {
			if meta.TsMin < minTS {
				minTS = meta.TsMin
			}
			if meta.TsMax > maxTS {
				maxTS = meta.TsMax
			}
			for _, keyword := range meta.FW {
				keywords[keyword] = true
			}
		}
		fw := make([]string, 0, len(keywords))
		for keyword := range keywords {
			fw = append(fw, keyword)
		}
		sort.Strings(fw)
		summaries = append(summaries, alis.BlockSummary{BlockHeight: uint64(len(summaries)), LIDs: []uint64{uint64(lid)}, FileKeywords: fw, FileTsMin: minTS, FileTsMax: maxTS})
		blockIndex, err := alis.BuildBlockLevel(fanout, summaries)
		if err != nil {
			return result, err
		}
		lg, err := blockIndex.LG.Root()
		if err != nil {
			return result, err
		}
		fk, err := blockIndex.FK.Root()
		if err != nil {
			return result, err
		}
		bmin, err := blockIndex.FileTsMin.Root()
		if err != nil {
			return result, err
		}
		bmax, err := blockIndex.FileTsMax.Root()
		if err != nil {
			return result, err
		}
		if lg != rh.BlockLGRoot || fk != rh.BlockFKRoot || bmin != rh.BlockFileTsMinRoot || bmax != rh.BlockFileTsMaxRoot {
			return result, fmt.Errorf("blas live: block %d block-level roots mismatch", height)
		}
		result.Blocks++
	}
	if result.Records != uint64(records) || result.Files != uint64(files) || result.Blocks != uint64(blocks) {
		return result, errors.New("blas live: run counts mismatch")
	}
	if !reflect.DeepEqual(gen.KeywordUniverse(), ch.KeywordRegistry()[uint64(lid)]) {
		return result, errors.New("blas live: keyword registry mismatch")
	}
	return result, nil
}
