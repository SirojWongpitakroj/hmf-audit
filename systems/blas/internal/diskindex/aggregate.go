package diskindex

import (
	"fmt"

	"blas/optimize"
)

func (r *Reader) AggregateProof(index uint64) (optimize.AggProof, bool) {
	if index >= r.Meta.Leaves {
		return optimize.AggProof{}, false
	}
	var n node
	if !r.get(fmt.Sprintf("node/0/%d", index), &n) {
		return optimize.AggProof{}, false
	}
	p := optimize.AggProof{LeafIndex: int(index), LeafHash: n.Hash}
	pos := index
	width := r.Meta.Leaves
	for level := uint64(0); width > 1; level++ {
		start := pos / uint64(r.Meta.Fanout) * uint64(r.Meta.Fanout)
		step := optimize.AggProofStep{ChildIndex: int(pos - start)}
		for i := start; i < min(start+uint64(r.Meta.Fanout), width); i++ {
			if i == pos {
				continue
			}
			var sibling node
			if !r.get(fmt.Sprintf("node/%d/%d", level, i), &sibling) {
				return optimize.AggProof{}, false
			}
			step.SiblingHashes = append(step.SiblingHashes, sibling.Hash)
		}
		p.Steps = append(p.Steps, step)
		pos /= uint64(r.Meta.Fanout)
		width = (width + uint64(r.Meta.Fanout) - 1) / uint64(r.Meta.Fanout)
	}
	return p, r.Err() == nil
}
