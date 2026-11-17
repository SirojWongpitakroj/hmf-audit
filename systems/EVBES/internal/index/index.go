// Package index is the inverted index In (Eq. 9-10): PRF(h(w)) -> {LID, ...}.
//
// The paper's "PID" (Eq. 9-10) and "ID" (p. 8 example) are the Phase 3 LID
// (B4). Alg. 2 insertion and RetrieveFromIndex (Eq. 24) operate on a tree
// sub-node and live in package tree; putting them here would make index and
// tree import each other.
package index

import (
	"encoding/hex"
	"slices"
)

// InvertedIndex maps hex(PRF(h(w))) to the LIDs whose log contains w.
type InvertedIndex struct {
	Entries map[string][]string
}

// New is create_inverted_index() (Alg. 1 line 6).
func New() *InvertedIndex { return &InvertedIndex{Entries: map[string][]string{}} }

// Key is the map key used for a trapdoor.
func Key(tw []byte) string { return hex.EncodeToString(tw) }

// Has reports whether an entry exists for tw (Alg. 2 line 10).
func (ix *InvertedIndex) Has(tw []byte) bool {
	_, ok := ix.Entries[Key(tw)]
	return ok
}

// Init creates an empty entry for tw (Alg. 2 line 11, INIT_INVERTED_INDEX).
func (ix *InvertedIndex) Init(tw []byte) {
	if !ix.Has(tw) {
		ix.Entries[Key(tw)] = []string{}
	}
}

// Add appends lid under tw (Alg. 2 line 13, ADD_TO_INVERTED_INDEX). A LID
// already present under tw is not appended twice.
func (ix *InvertedIndex) Add(tw []byte, lid string) {
	k := Key(tw)
	if slices.Contains(ix.Entries[k], lid) {
		return
	}
	ix.Entries[k] = append(ix.Entries[k], lid)
}

// Get returns a copy of the LIDs under tw (nil if absent).
func (ix *InvertedIndex) Get(tw []byte) []string {
	return slices.Clone(ix.Entries[Key(tw)])
}

// AllLIDs returns every LID referenced anywhere in the index, sorted and
// de-duplicated.
func (ix *InvertedIndex) AllLIDs() []string {
	var all []string
	for _, lids := range ix.Entries {
		all = append(all, lids...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

// SortedKeys returns the entry keys in ascending order (for deterministic
// serialization; Go maps are never iterated for output).
func (ix *InvertedIndex) SortedKeys() []string {
	keys := make([]string, 0, len(ix.Entries))
	for k := range ix.Entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
