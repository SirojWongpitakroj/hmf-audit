package canonicalingestion

func (m *AccessMetrics) ObserveIndexBatch(rows, bytes int64) {
	peak(&m.PeakIndexBatchRows, rows)
	peak(&m.PeakIndexBatchBytes, bytes)
}
