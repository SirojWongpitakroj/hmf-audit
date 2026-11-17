package chain

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

type persistentChain struct {
	Fanout           int
	SegmentSize      uint64
	UseAggregation   bool
	Blocks           []*Block
	Segments         []*Segment
	Current          *Segment
	HavePastTsMax    bool
	PastTsMaxRunning int64
	KeywordRegistry  map[uint64][]string
}

// GobEncode serializes the proof-ready full-node state. Authenticated indexes
// retain their built levels through their own explicit encodings.
func (c Chain) GobEncode() ([]byte, error) {
	state := persistentChain{
		Fanout: c.fanout, SegmentSize: c.segmentSize,
		UseAggregation: c.useAggregation, Blocks: c.blocks,
		Segments: c.segments, Current: c.current,
		HavePastTsMax: c.havePastTsMax, PastTsMaxRunning: c.pastTsMaxRunning,
		KeywordRegistry: c.keywordRegistry,
	}
	var output bytes.Buffer
	err := gob.NewEncoder(&output).Encode(state)
	return output.Bytes(), err
}

// GobDecode restores a proof-ready full-node snapshot.
func (c *Chain) GobDecode(data []byte) error {
	var state persistentChain
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		return err
	}
	if state.Fanout < 2 || state.SegmentSize == 0 {
		return fmt.Errorf("chain: invalid persisted geometry")
	}
	*c = Chain{
		fanout: state.Fanout, segmentSize: state.SegmentSize,
		useAggregation: state.UseAggregation, blocks: state.Blocks,
		segments: state.Segments, current: state.Current,
		havePastTsMax: state.HavePastTsMax, pastTsMaxRunning: state.PastTsMaxRunning,
		keywordRegistry: state.KeywordRegistry,
	}
	return nil
}
