package durable

import (
	"blas/bitmap"
	"blas/internal/diskindex"
)

func (w *writer) keywordInput(word string) diskindex.PreparedKeyword {
	if input, ok := w.keywordInputs[word]; ok {
		if w.a.Metrics != nil {
			w.a.Metrics.Access.KeywordEncodingsReused.Add(1)
		}
		return input
	}
	input := diskindex.PrepareKeyword(word)
	cost := 2*len(word) + 128
	zeroBytes := 0
	if w.zeroBitmap != nil {
		zeroBytes = int((w.zeroBitmap.Len()+7)/8) + 128
	}
	if len(w.keywordInputs) < w.a.Execution.CacheMaxEntries && cost <= w.a.Execution.CacheMaxBytes-w.keywordInputBytes-zeroBytes {
		if w.keywordInputs == nil {
			w.keywordInputs = make(map[string]diskindex.PreparedKeyword)
		}
		w.keywordInputs[word] = input
		w.keywordInputBytes += cost
	}
	return input
}

func (w *writer) zeroInput(n uint64) diskindex.PreparedBitmap {
	if w.zeroBitmap != nil && w.zeroBitmap.Len() == n {
		if w.a.Metrics != nil {
			w.a.Metrics.Access.ZeroBitmapComputationsReused.Add(1)
		}
		return *w.zeroBitmap
	}
	input := diskindex.PrepareBitmap(bitmap.New(n))
	// Retain only one population, never a map of historical bitmap lengths.
	w.zeroBitmap = nil
	if int((n+7)/8)+128 <= w.a.Execution.CacheMaxBytes-w.keywordInputBytes {
		w.zeroBitmap = &input
	}
	return input
}
