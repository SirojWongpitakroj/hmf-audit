package durable

func (w *writer) lowRiskEnabled() bool {
	return w.accessOptimized() && !w.a.DisableLowRiskOptimizations
}

func rowOwner(rows [][]any) uint64 { return uint64(rows[0][2].(int64)) }

// Only closeBlock's tx conversion loop uses this buffer. That loop has no
// tx-object readers until its flush barrier. No entry/block/index write is deferred.
func (w *writer) bufferTransaction(row []any, owner, oid uint64, words, lids []string, minTS, maxTS int64) error {
	e := w.a.Execution
	// Account row parameters plus the pending metadata references conservatively.
	cost := 9*32 + 4*16 + len(row[0].(string)) + len("tx") + len(row[4].([]byte)) + len(row[5].(string)) + len(row[6].(string)) + summaryCost(words, lids)
	if len(w.objectRows) > 0 && (w.objectRows[0][1] != "tx" || rowOwner(w.objectRows) != owner || len(w.objectRows) >= e.BatchMaxRecords || cost > e.BatchMaxBytes-w.transactionBytes) {
		if err := w.flushObjects(); err != nil {
			return err
		}
	}
	w.objectRows = append(w.objectRows, row)
	if cost > e.BatchMaxBytes {
		// Preserve the old single-row acceptance limit; a conservative decoded
		// scratch estimate must never reject a previously accepted SQL row.
		if err := w.flushObjects(); err != nil {
			return err
		}
		w.extendMetadata("tx", owner, oid, words, lids, minTS, maxTS)
		return nil
	}
	w.transactionMetadata = append(w.transactionMetadata, objectSummary{oid: oid, keywords: words, lids: lids, min: minTS, max: maxTS})
	w.transactionBytes += cost
	if w.a.Metrics != nil {
		w.observeTransactionBytes()
	}
	if len(w.objectRows) >= e.BatchMaxRecords {
		return w.flushObjects()
	}
	return nil
}

func (w *writer) observeTransactionBytes() {
	p := &w.a.Metrics.Access.PeakTransactionObjectBytes
	for old := p.Load(); int64(w.transactionBytes) > old; old = p.Load() {
		if p.CompareAndSwap(old, int64(w.transactionBytes)) {
			break
		}
	}
}
