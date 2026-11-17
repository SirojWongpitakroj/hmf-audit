package omt

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"blas/bhash"
)

type persistentNode struct {
	OID     uint64
	Object  []byte
	ObjHash bhash.Digest
	Hash    bhash.Digest
}

type persistentTree struct {
	Fanout int
	Levels [][]persistentNode
}

// GobEncode serializes the proof-ready OMT state for a persistent full-node
// snapshot. It stores the built levels rather than rebuilding an index per
// audit request.
func (t Tree) GobEncode() ([]byte, error) {
	state := persistentTree{Fanout: t.fanout, Levels: make([][]persistentNode, len(t.levels))}
	for level, nodes := range t.levels {
		state.Levels[level] = make([]persistentNode, len(nodes))
		for index, node := range nodes {
			state.Levels[level][index] = persistentNode{
				OID: node.oid, Object: append([]byte(nil), node.object...),
				ObjHash: node.objHash, Hash: node.hash,
			}
		}
	}
	var output bytes.Buffer
	err := gob.NewEncoder(&output).Encode(state)
	return output.Bytes(), err
}

// GobDecode restores a proof-ready OMT snapshot.
func (t *Tree) GobDecode(data []byte) error {
	var state persistentTree
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		return err
	}
	if state.Fanout < 2 {
		return fmt.Errorf("omt: persisted fanout must be >= 2")
	}
	t.fanout = state.Fanout
	t.levels = make([][]node, len(state.Levels))
	for level, nodes := range state.Levels {
		t.levels[level] = make([]node, len(nodes))
		for index, saved := range nodes {
			t.levels[level][index] = node{
				oid: saved.OID, object: append([]byte(nil), saved.Object...),
				objHash: saved.ObjHash, hash: saved.Hash,
			}
		}
	}
	return nil
}
