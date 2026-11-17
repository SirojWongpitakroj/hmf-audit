package optimize

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"blas/bhash"
)

type persistentAggregatedTree struct {
	Fanout int
	Levels [][]bhash.Digest
}

// GobEncode serializes a built aggregation tree without regenerating it.
func (t AggregatedTree) GobEncode() ([]byte, error) {
	var output bytes.Buffer
	err := gob.NewEncoder(&output).Encode(persistentAggregatedTree{
		Fanout: t.fanout, Levels: t.levels,
	})
	return output.Bytes(), err
}

// GobDecode restores a built aggregation tree.
func (t *AggregatedTree) GobDecode(data []byte) error {
	var state persistentAggregatedTree
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		return err
	}
	if state.Fanout < 2 {
		return fmt.Errorf("optimize: persisted fanout must be >= 2")
	}
	t.fanout = state.Fanout
	t.levels = mtLevels(state.Levels)
	return nil
}
