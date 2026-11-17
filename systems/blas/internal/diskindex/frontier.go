package diskindex

import (
	"blas/bhash"
	"context"
	"fmt"
)

// The frontier keeps at most fanout-1 nodes per level. It emits exactly the
// original level/index positions and combines partial groups only at Finish.
// No previously persisted tree or historical page is loaded for construction.
func (b *Builder) parent(children []node) node {
	parent := node{Min: children[0].Min}
	parts := make([][]byte, 0, len(children)+1)
	if b.Meta.Kind == "rimbt" {
		var values []byte
		for _, child := range children[1:] {
			parent.V = append(parent.V, child.Min)
			values = append(values, bhash.Int64BE(child.Min)...)
		}
		parts = append(parts, values)
	}
	for i := range children {
		parts = append(parts, children[i].Hash[:])
	}
	parent.Hash = b.hashConcat(parts...)
	return parent
}
func (b *Builder) emit(ctx context.Context, level int, n node) error {
	for len(b.frontier) <= level {
		b.frontier = append(b.frontier, nil)
		b.widths = append(b.widths, 0)
	}
	pos := b.widths[level]
	if err := b.Store.putJSON(ctx, b.Tree, fmt.Sprintf("node/%d/%d", level, pos), n); err != nil {
		return err
	}
	b.widths[level]++
	b.frontier[level] = append(b.frontier[level], n)
	if len(b.frontier[level]) == b.Meta.Fanout {
		parent := b.parent(b.frontier[level])
		b.frontier[level] = nil
		return b.emit(ctx, level+1, parent)
	}
	return nil
}
func (b *Builder) finishFrontier(ctx context.Context) (Metadata, error) {
	for level := 0; level < len(b.frontier); level++ {
		children := b.frontier[level]
		if len(children) == 0 {
			continue
		}
		higher := false
		for i := level + 1; i < len(b.frontier); i++ {
			if len(b.frontier[i]) > 0 {
				higher = true
				break
			}
		}
		if len(children) == 1 && !higher {
			b.Meta.Root = children[0].Hash
			if err := b.Store.putJSON(ctx, b.Tree, "metadata", b.Meta); err != nil {
				return Metadata{}, err
			}
			if err := b.Store.Batch.FinishTree(ctx); err != nil {
				return Metadata{}, err
			}
			return b.Meta, nil
		}
		parent := b.parent(children)
		b.frontier[level] = nil
		if err := b.emit(ctx, level+1, parent); err != nil {
			return Metadata{}, err
		}
	}
	return Metadata{}, fmt.Errorf("diskindex: missing streamed root")
}
