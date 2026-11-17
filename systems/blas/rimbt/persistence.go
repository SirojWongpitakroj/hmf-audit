package rimbt

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"blas/bhash"
	"blas/bitmap"
)

type persistentLeaf struct {
	Value  int64
	Bitmap bitmap.Bitmap
	Hash   bhash.Digest
}

type persistentNode struct {
	Hash     bhash.Digest
	V        []int64
	MinValue int64
}

type persistentTree struct {
	Fanout int
	N      uint64
	Leaves []persistentLeaf
	Levels [][]persistentNode
}

// GobEncode serializes the built RIMBT used by a persistent full node.
func (t Tree) GobEncode() ([]byte, error) {
	state := persistentTree{
		Fanout: t.fanout, N: t.n,
		Leaves: make([]persistentLeaf, len(t.leaves)),
		Levels: make([][]persistentNode, len(t.levels)),
	}
	for index, leaf := range t.leaves {
		state.Leaves[index] = persistentLeaf{
			Value: leaf.v, Bitmap: leaf.bm, Hash: leaf.hash,
		}
	}
	for level, nodes := range t.levels {
		state.Levels[level] = make([]persistentNode, len(nodes))
		for index, node := range nodes {
			state.Levels[level][index] = persistentNode{
				Hash: node.hash, V: append([]int64(nil), node.v...), MinValue: node.minValue,
			}
		}
	}
	var output bytes.Buffer
	err := gob.NewEncoder(&output).Encode(state)
	return output.Bytes(), err
}

// GobDecode restores a built RIMBT snapshot.
func (t *Tree) GobDecode(data []byte) error {
	var state persistentTree
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		return err
	}
	if state.Fanout < 2 {
		return fmt.Errorf("rimbt: persisted fanout must be >= 2")
	}
	*t = Tree{
		fanout: state.Fanout, n: state.N,
		leaves: make([]leafRec, len(state.Leaves)),
		levels: make([][]nodeInfo, len(state.Levels)),
	}
	for index, saved := range state.Leaves {
		t.leaves[index] = leafRec{v: saved.Value, bm: saved.Bitmap, hash: saved.Hash}
	}
	for level, nodes := range state.Levels {
		t.levels[level] = make([]nodeInfo, len(nodes))
		for index, saved := range nodes {
			t.levels[level][index] = nodeInfo{
				hash: saved.Hash, v: append([]int64(nil), saved.V...), minValue: saved.MinValue,
			}
		}
	}
	return nil
}
