package iimt

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"blas/bhash"
	"blas/bitmap"
)

type persistentLeaf struct {
	KID     uint64
	Keyword string
	Bitmap  bitmap.Bitmap
	Hash    bhash.Digest
}

type persistentTree struct {
	Fanout int
	N      uint64
	Leaves []persistentLeaf
	Levels [][]bhash.Digest
	Dirty  bool
}

// GobEncode serializes the built IIMT used by a persistent full node.
func (t Tree) GobEncode() ([]byte, error) {
	state := persistentTree{
		Fanout: t.fanout, N: t.n, Levels: t.levels, Dirty: t.dirty,
		Leaves: make([]persistentLeaf, len(t.leaves)),
	}
	for index, leaf := range t.leaves {
		state.Leaves[index] = persistentLeaf{
			KID: leaf.kid, Keyword: leaf.keyword, Bitmap: leaf.bm, Hash: leaf.hash,
		}
	}
	var output bytes.Buffer
	err := gob.NewEncoder(&output).Encode(state)
	return output.Bytes(), err
}

// GobDecode restores the built IIMT and its lookup accelerator.
func (t *Tree) GobDecode(data []byte) error {
	var state persistentTree
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		return err
	}
	if state.Fanout < 2 {
		return fmt.Errorf("iimt: persisted fanout must be >= 2")
	}
	*t = Tree{
		fanout: state.Fanout, n: state.N, levels: state.Levels, dirty: state.Dirty,
		leaves: make([]leafState, len(state.Leaves)),
		kwIndex: make(map[string]int, len(state.Leaves)),
	}
	for index, saved := range state.Leaves {
		if _, duplicate := t.kwIndex[saved.Keyword]; duplicate {
			return fmt.Errorf("iimt: duplicate persisted keyword %q", saved.Keyword)
		}
		t.leaves[index] = leafState{
			kid: saved.KID, keyword: saved.Keyword, bm: saved.Bitmap, hash: saved.Hash,
		}
		t.kwIndex[saved.Keyword] = index
	}
	return nil
}
