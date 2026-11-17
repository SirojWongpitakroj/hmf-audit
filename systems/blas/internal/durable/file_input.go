package durable

import (
	"blas/bhash"
	"blas/internal/diskindex"
)

type fileInputEntry struct {
	object []byte
	words  []string
}

// Private to one Apply/SQL transaction. A partial file inherited from an earlier
// transaction is deliberately not cached. No cache survives rollback or commit.
type currentFileInput struct {
	owner   uint64
	entries []fileInputEntry
	bytes   int
}

func (w *writer) rememberFileInput(owner, oid uint64, object []byte, words []string) {
	if oid == 0 {
		w.fileInput = &currentFileInput{owner: owner}
	}
	f := w.fileInput
	if f == nil {
		return
	}
	cost := len(object) + summaryCost(words, nil) + 256
	if f.owner != owner || uint64(len(f.entries)) != oid || len(f.entries) >= w.a.Execution.CacheMaxEntries || len(f.entries) >= w.a.Options.EntriesPerFile || cost > w.a.Execution.CacheMaxBytes-f.bytes {
		w.fileInput = nil
		return
	}
	// EncodeLogEntry returns newly owned bytes; the ordered writer and SQL insert
	// only read them. Copy the keyword slice so temporary callers cannot alias it.
	f.entries = append(f.entries, fileInputEntry{object: object, words: append([]string(nil), words...)})
	f.bytes += cost
	if w.a.Metrics != nil {
		p := &w.a.Metrics.Access.PeakFileInputBytes
		for old := p.Load(); int64(f.bytes) > old; old = p.Load() {
			if p.CompareAndSwap(old, int64(f.bytes)) {
				break
			}
		}
	}
}

func (w *writer) completeFileInput(owner, n uint64) *currentFileInput {
	if f := w.fileInput; w.lowRiskEnabled() && f != nil && f.owner == owner && uint64(len(f.entries)) == n {
		return f
	}
	return nil
}

func (w *writer) cachedFileObjects(tree string, f *currentFileInput) (bhash.Digest, error) {
	b, err := diskindex.NewBuilder(w.pages, tree, "omt", w.a.Options.Fanout, uint64(len(f.entries)))
	if err != nil {
		return bhash.Digest{}, err
	}
	for oid, entry := range f.entries {
		if err := b.AddObject(w.ctx, uint64(oid), entry.object); err != nil {
			return bhash.Digest{}, err
		}
	}
	meta, err := b.Finish(w.ctx)
	if err == nil && w.a.Metrics != nil {
		w.a.Metrics.Access.EntryObjectRereadsAvoided.Add(uint64(len(f.entries)))
	}
	return meta.Root, err
}
