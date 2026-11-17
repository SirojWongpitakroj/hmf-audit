// Pre-optimization Apply retained only as a compatibility/performance oracle.
package durable

import (
	"blas/alis"
	"blas/internal/diskindex"
	"blas/live"
	contract "canonicalingestion"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

func (a Adapter) applySequentialReference(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
	var st state
	if len(p.Checkpoint) > 0 {
		if err := json.Unmarshal(p.Checkpoint, &st); err != nil {
			return nil, nil, err
		}
	}
	if st.Records != p.Committed {
		return nil, nil, errors.New("blas durable: native count differs from canonical cursor")
	}
	w := writer{a: a, tx: tx, ctx: ctx, state: st, pages: diskindex.Store{Q: tx, Scope: a.Options.RunID, Limits: a.Options.Index}}
	ids := make([]string, len(entries))
	for i, e := range entries {
		if len(e.Record) > a.Options.ObjectBytes {
			return nil, nil, errors.New("blas durable: canonical object byte limit exceeded")
		}
		var o live.Observation
		if err := json.Unmarshal(e.Record, &o); err != nil {
			return nil, nil, err
		}
		if err := o.Validate(); err != nil {
			return nil, nil, err
		}
		ts := o.Timestamp.UnixMilli()
		if w.state.InFile > 0 && (w.state.InFile >= uint64(a.Options.EntriesPerFile) || ts < w.state.LastTS) {
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
		keywords := o.Keywords()
		for _, word := range keywords {
			var kid int64
			err := tx.QueryRowContext(ctx, `INSERT INTO blas.durable_keywords(run_id,kid,keyword) VALUES($1,$2,$3) ON CONFLICT(run_id,keyword) DO NOTHING RETURNING kid`, a.Options.RunID, int64(w.state.NextKID), word).Scan(&kid)
			if err == nil {
				w.state.NextKID++
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, nil, err
			}
		}
		entry := alis.LogEntry{ESN: w.state.InFile, TS: ts, EW: keywords, V: e.Record}
		object := alis.EncodeLogEntry(entry)
		if len(object) > a.Options.ObjectBytes {
			return nil, nil, errors.New("blas durable: encoded object byte limit exceeded")
		}
		if err := w.object("entry", w.state.Files, w.state.InFile, object, keywords, []string{fmt.Sprint(a.lid())}, ts, ts); err != nil {
			return nil, nil, err
		}
		ids[i] = fmt.Sprintf("%d/%d/%d", a.lid(), w.state.Files, w.state.InFile)
		w.state.InFile++
		w.state.Records++
		w.state.LastTS = ts
		if w.state.InFile == uint64(a.Options.EntriesPerFile) {
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
	}
	// Close native immutable units only at natural limits, timestamp reversal, or
	// an explicit dataset milestone. Transaction batches/restarts add no boundary.
	if w.state.Records == p.Target {
		if err := w.closeFile(); err != nil {
			return nil, nil, err
		}
		if err := w.closeBlock(); err != nil {
			return nil, nil, err
		}
	}
	checkpoint, err := json.Marshal(w.state)
	return ids, checkpoint, err
}
