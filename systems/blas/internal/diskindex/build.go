package diskindex

import (
	"context"
	"fmt"

	"blas/bhash"
	"blas/bitmap"
)

// Builder accepts one leaf at a time. Only one bitmap/object and one fanout
// group are resident. A bitmap's chunks are storage fragments; its hash remains
// H(the original complete uncompressed bitmap), not a new chunk commitment.
type Builder struct {
	Store     Store
	Tree      string
	Meta      Metadata
	lastValue int64
	haveValue bool
	frontier  [][]node
	widths    []uint64
}

func NewBuilder(s Store, tree, kind string, fanout int, n uint64) (*Builder, error) {
	if err := s.Limits.Validate(); err != nil {
		return nil, err
	}
	if fanout < 2 || fanout > 1024 {
		return nil, fmt.Errorf("diskindex: invalid fanout")
	}
	if kind != "iimt" && kind != "rimbt" && kind != "omt" && kind != "aggregate" {
		return nil, fmt.Errorf("diskindex: unknown tree kind")
	}
	if (n+7)/8 > uint64(s.Limits.BitmapBytes) {
		return nil, fmt.Errorf("diskindex: bitmap working-set budget exceeded")
	}
	return &Builder{Store: s, Tree: tree, Meta: Metadata{Kind: kind, Fanout: fanout, N: n, ZeroMarkers: s.CompactZeroBitmaps}}, nil
}
func (b *Builder) AddBitmap(ctx context.Context, keyword string, value int64, bm bitmap.Bitmap) error {
	if bm.Len() != b.Meta.N {
		return fmt.Errorf("diskindex: bitmap population differs")
	}
	if b.Meta.Kind != "iimt" && b.Meta.Kind != "rimbt" {
		return fmt.Errorf("diskindex: not a bitmap tree")
	}
	if b.Meta.Kind == "rimbt" && b.haveValue && value <= b.lastValue {
		return fmt.Errorf("diskindex: values must be unique and sorted")
	}
	data := bm.Bytes()
	inner := b.hash(data)
	var hash bhash.Digest
	if b.Meta.Kind == "iimt" {
		hash = b.hashConcat(bhash.FramedString(keyword), inner[:])
	} else {
		hash = b.hashConcat(bhash.Int64BE(value), inner[:])
	}
	return b.addBitmap(ctx, keyword, value, data, hash)
}

// addBitmap preserves every original leaf, bitmap fragment, lookup row and node.
// Callers may reuse immutable hash inputs, never the keyword-dependent leaf hash.
func (b *Builder) addBitmap(ctx context.Context, keyword string, value int64, data []byte, hash bhash.Digest) error {
	zero := b.Store.CompactZeroBitmaps
	if zero {
		for _, v := range data {
			if v != 0 {
				zero = false
				break
			}
		}
	}
	if err := b.Store.putJSON(ctx, b.Tree, fmt.Sprintf("leaf/%d", b.Meta.Leaves), leaf{Keyword: []byte(keyword), Value: value, Zero: zero}); err != nil {
		return err
	}
	for offset := 0; offset < len(data); offset += b.Store.Limits.PageBytes {
		if zero {
			break
		}
		end := min(offset+b.Store.Limits.PageBytes, len(data))
		if err := b.Store.Put(ctx, b.Tree, fmt.Sprintf("bitmap/%d/%d", b.Meta.Leaves, offset/b.Store.Limits.PageBytes), data[offset:end]); err != nil {
			return err
		}
	}
	var writeErr error
	if b.Store.Batch != nil {
		writeErr = b.Store.Batch.leaf(ctx, b.Store.Scope, b.Tree, int64(b.Meta.Leaves), []byte(keyword), value)
	} else {
		_, writeErr = b.Store.Q.ExecContext(ctx, `INSERT INTO blas.index_leaves(scope,tree,pos,keyword,value) VALUES($1,$2,$3,$4,$5)`, b.Store.Scope, b.Tree, int64(b.Meta.Leaves), []byte(keyword), value)
	}
	if writeErr != nil {
		return writeErr
	}
	var nodeErr error
	if b.Store.Batch != nil {
		nodeErr = b.emit(ctx, 0, node{Hash: hash, Min: value})
	} else {
		nodeErr = b.Store.putJSON(ctx, b.Tree, fmt.Sprintf("node/0/%d", b.Meta.Leaves), node{Hash: hash, Min: value})
	}
	if nodeErr != nil {
		return nodeErr
	}
	b.lastValue = value
	b.haveValue = true
	b.Meta.Leaves++
	return nil
}
func (b *Builder) AddObject(ctx context.Context, oid uint64, object []byte) error {
	if b.Meta.Kind != "omt" || oid != b.Meta.Leaves {
		return fmt.Errorf("diskindex: OMT requires sequential object IDs")
	}
	// Objects can span pages; leaf metadata never embeds an unbounded object.
	if err := b.Store.putJSON(ctx, b.Tree, fmt.Sprintf("leaf/%d", oid), leaf{OID: oid, Value: int64(len(object))}); err != nil {
		return err
	}
	for offset := 0; offset < len(object); offset += b.Store.Limits.PageBytes {
		if err := b.Store.Put(ctx, b.Tree, fmt.Sprintf("object/%d/%d", oid, offset/b.Store.Limits.PageBytes), object[offset:min(offset+b.Store.Limits.PageBytes, len(object))]); err != nil {
			return err
		}
	}
	h := bhash.H(object)
	return b.AddHash(ctx, bhash.HashConcat(bhash.Uint64BE(oid), h[:]))
}
func (b *Builder) AddHash(ctx context.Context, h bhash.Digest) error {
	if b.Meta.Kind != "omt" && b.Meta.Kind != "aggregate" {
		return fmt.Errorf("diskindex: not a hash tree")
	}
	if b.Store.Batch != nil {
		if err := b.emit(ctx, 0, node{Hash: h}); err != nil {
			return err
		}
		b.Meta.Leaves++
		return nil
	}
	if err := b.Store.putJSON(ctx, b.Tree, fmt.Sprintf("node/0/%d", b.Meta.Leaves), node{Hash: h}); err != nil {
		return err
	}
	b.Meta.Leaves++
	return nil
}
func (b *Builder) Finish(ctx context.Context) (Metadata, error) {
	if b.Meta.Leaves == 0 {
		return Metadata{}, fmt.Errorf("diskindex: empty tree")
	}
	if b.Store.Batch != nil {
		return b.finishFrontier(ctx)
	}
	width := b.Meta.Leaves
	level := uint64(0)
	for width > 1 {
		parents := (width + uint64(b.Meta.Fanout) - 1) / uint64(b.Meta.Fanout)
		for p := uint64(0); p < parents; p++ {
			start := p * uint64(b.Meta.Fanout)
			end := min(start+uint64(b.Meta.Fanout), width)
			parts := make([][]byte, 0, b.Meta.Fanout+1)
			var parent node
			for i := start; i < end; i++ {
				var child node
				if err := b.Store.getJSON(ctx, b.Tree, fmt.Sprintf("node/%d/%d", level, i), &child); err != nil {
					return Metadata{}, err
				}
				if i == start {
					parent.Min = child.Min
				} else if b.Meta.Kind == "rimbt" {
					parent.V = append(parent.V, child.Min)
				}
				parts = append(parts, append([]byte(nil), child.Hash[:]...))
			}
			if b.Meta.Kind == "rimbt" {
				var v []byte
				for _, x := range parent.V {
					v = append(v, bhash.Int64BE(x)...)
				}
				parts = append([][]byte{v}, parts...)
			}
			parent.Hash = bhash.HashConcat(parts...)
			if err := b.Store.putJSON(ctx, b.Tree, fmt.Sprintf("node/%d/%d", level+1, p), parent); err != nil {
				return Metadata{}, err
			}
		}
		width = parents
		level++
	}
	h, err := b.Store.Hash(ctx, b.Tree, level, 0)
	if err != nil {
		return Metadata{}, err
	}
	b.Meta.Root = h
	err = b.Store.putJSON(ctx, b.Tree, "metadata", b.Meta)
	return b.Meta, err
}
