package diskindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/omt"
	"blas/rimbt"
)

// Budget is request-scoped, including cached reads. The retained proof and
// bitmap working set must also be bounded by the caller's query budget.
type Budget struct {
	MaxBytes, ReadBytes int
	Err                 error
}
type Reader struct {
	Store  Store
	Tree   string
	Meta   Metadata
	Ctx    context.Context
	Budget *Budget
	err    error
}

func Open(ctx context.Context, s Store, tree string, budget *Budget) (*Reader, error) {
	if budget == nil || budget.MaxBytes <= 0 {
		return nil, errors.New("diskindex: request byte budget required")
	}
	r := &Reader{Store: s, Tree: tree, Ctx: ctx, Budget: budget}
	if err := s.getJSON(ctx, tree, "metadata", &r.Meta); err != nil {
		return nil, err
	}
	if r.Meta.Fanout < 2 || r.Meta.Leaves == 0 || (r.Meta.N+7)/8 > uint64(s.Limits.BitmapBytes) {
		return nil, errors.New("diskindex: invalid metadata or bitmap budget")
	}
	return r, nil
}
func (r *Reader) Err() error {
	if r.err != nil {
		return r.err
	}
	return r.Budget.Err
}
func (r *Reader) charge(n int) bool {
	if r.Err() != nil {
		return false
	}
	if n < 0 || n > r.Budget.MaxBytes-r.Budget.ReadBytes {
		r.Budget.Err = errors.New("diskindex: request byte budget exceeded")
		return false
	}
	r.Budget.ReadBytes += n
	return true
}
func (r *Reader) get(page string, v any) bool {
	if r.Err() != nil {
		return false
	}
	b, err := r.Store.Get(r.Ctx, r.Tree, page)
	if err != nil {
		r.err = err
		return false
	}
	if !r.charge(len(b)) {
		return false
	}
	if err = json.Unmarshal(b, v); err != nil {
		r.err = err
		return false
	}
	return true
}
func (r *Reader) Root() (bhash.Digest, error) { return r.Meta.Root, r.Err() }
func (r *Reader) N() uint64                   { return r.Meta.N }
func (r *Reader) Fanout() int                 { return r.Meta.Fanout }
func (r *Reader) NumLeaves() int              { return int(r.Meta.Leaves) }
func (r *Reader) bitmap(pos uint64) bitmap.Bitmap {
	size := (r.Meta.N + 7) / 8
	if size > uint64(r.Store.Limits.BitmapBytes) || !r.charge(int(size)*2) {
		return bitmap.Bitmap{}
	}
	data := make([]byte, 0, int(size))
	if r.Meta.ZeroMarkers {
		var l leaf
		if !r.get(fmt.Sprintf("leaf/%d", pos), &l) {
			return bitmap.Bitmap{}
		}
		if l.Zero {
			return bitmap.New(r.Meta.N)
		}
	}
	for i := uint64(0); uint64(len(data)) < size; i++ {
		b, err := r.Store.Get(r.Ctx, r.Tree, fmt.Sprintf("bitmap/%d/%d", pos, i))
		if err != nil {
			r.err = err
			return bitmap.Bitmap{}
		}
		if len(b) == 0 || uint64(len(b)) > size-uint64(len(data)) {
			r.err = errors.New("diskindex: invalid bitmap chunk")
			return bitmap.Bitmap{}
		}
		data = append(data, b...)
	}
	bm, err := bitmap.FromBytes(r.Meta.N, data)
	if err != nil {
		r.err = err
	}
	return bm
}
func (r *Reader) KID(w string) (uint64, bool) {
	var p int64
	err := r.Store.Row(r.Ctx, `SELECT pos FROM blas.index_leaves WHERE scope=$1 AND tree=$2 AND keyword=$3 ORDER BY pos LIMIT 1`, r.Store.Scope, r.Tree, []byte(w)).Scan(&p)
	if r.Store.Cache != nil {
		r.Store.Cache.read()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		r.err = err
		return 0, false
	}
	if p < 0 || uint64(p) >= r.Meta.Leaves {
		r.err = errors.New("diskindex: invalid leaf position")
		return 0, false
	}
	return uint64(p), true
}
func (r *Reader) iimtProof(pos uint64) iimt.Proof {
	var l leaf
	if !r.get(fmt.Sprintf("leaf/%d", pos), &l) {
		return iimt.Proof{}
	}
	p := iimt.Proof{Keyword: string(l.Keyword), Bitmap: r.bitmap(pos)}
	width := r.Meta.Leaves
	for level := uint64(0); width > 1; level++ {
		start := pos / uint64(r.Meta.Fanout) * uint64(r.Meta.Fanout)
		end := min(start+uint64(r.Meta.Fanout), width)
		step := iimt.ProofStep{ChildIndex: int(pos - start)}
		for i := start; i < end; i++ {
			if i == pos {
				continue
			}
			var n node
			if !r.get(fmt.Sprintf("node/%d/%d", level, i), &n) {
				return iimt.Proof{}
			}
			step.SiblingHashes = append(step.SiblingHashes, n.Hash)
		}
		p.Steps = append(p.Steps, step)
		pos /= uint64(r.Meta.Fanout)
		width = (width + uint64(r.Meta.Fanout) - 1) / uint64(r.Meta.Fanout)
	}
	return p
}
func (r *Reader) Query(w string) ([]uint64, iimt.Proof, bool) {
	p, ok := r.KID(w)
	if !ok {
		return nil, iimt.Proof{}, false
	}
	proof := r.iimtProof(p)
	if r.Err() != nil {
		return nil, iimt.Proof{}, false
	}
	indices := r.indices(proof.Bitmap)
	if r.Err() != nil {
		return nil, iimt.Proof{}, false
	}
	return indices, proof, true
}
func (r *Reader) ProveAbsent(w string) (iimt.AbsenceProof, bool) {
	if _, ok := r.KID(w); ok || r.Err() != nil {
		return iimt.AbsenceProof{}, false
	}
	p := iimt.AbsenceProof{Fanout: r.Meta.Fanout}
	for i := uint64(0); i < r.Meta.Leaves; i++ {
		var l leaf
		if !r.get(fmt.Sprintf("leaf/%d", i), &l) {
			return iimt.AbsenceProof{}, false
		}
		bm := r.bitmap(i)
		if r.Err() != nil {
			return iimt.AbsenceProof{}, false
		}
		p.Leaves = append(p.Leaves, iimt.AbsentLeaf{Keyword: string(l.Keyword), Bitmap: bm})
	}
	return p, true
}
func (r *Reader) rimbtProof(pos uint64) rimbt.Proof {
	var l leaf
	if !r.get(fmt.Sprintf("leaf/%d", pos), &l) {
		return rimbt.Proof{}
	}
	p := rimbt.Proof{V: l.Value, Bitmap: r.bitmap(pos)}
	width := r.Meta.Leaves
	for level := uint64(0); width > 1; level++ {
		start := pos / uint64(r.Meta.Fanout) * uint64(r.Meta.Fanout)
		end := min(start+uint64(r.Meta.Fanout), width)
		var parent node
		if !r.get(fmt.Sprintf("node/%d/%d", level+1, pos/uint64(r.Meta.Fanout)), &parent) {
			return rimbt.Proof{}
		}
		step := rimbt.ProofStep{ChildIndex: int(pos - start), V: parent.V}
		for i := start; i < end; i++ {
			if i == pos {
				continue
			}
			var n node
			if !r.get(fmt.Sprintf("node/%d/%d", level, i), &n) {
				return rimbt.Proof{}
			}
			step.SiblingHashes = append(step.SiblingHashes, n.Hash)
		}
		p.Steps = append(p.Steps, step)
		pos /= uint64(r.Meta.Fanout)
		width = (width + uint64(r.Meta.Fanout) - 1) / uint64(r.Meta.Fanout)
	}
	return p
}
func (r *Reader) boundary(v int64) (uint64, bool) {
	var p int64
	err := r.Store.Row(r.Ctx, `SELECT pos FROM blas.index_leaves WHERE scope=$1 AND tree=$2 AND value >= $3 ORDER BY value LIMIT 1`, r.Store.Scope, r.Tree, v).Scan(&p)
	if r.Store.Cache != nil {
		r.Store.Cache.read()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r.Meta.Leaves, false
	}
	if err != nil {
		r.err = err
		return 0, false
	}
	return uint64(p), true
}
func (r *Reader) RangeQuery(alpha, beta int64) (rimbt.RangeResult, error) {
	if !r.charge(int((r.Meta.N+7)/8) * 4) {
		return rimbt.RangeResult{}, r.Err()
	}
	if alpha > beta {
		return rimbt.RangeResult{}, rimbt.ErrInvertedRange
	}
	a, _ := r.boundary(alpha)
	b, hasB := r.boundary(beta)
	var out rimbt.RangeResult
	lower := bitmap.New(r.Meta.N)
	upper := bitmap.New(r.Meta.N)
	if a > 0 {
		out.LowerReal = true
		out.LowerProof = r.rimbtProof(a - 1)
		lower = out.LowerProof.Bitmap
	}
	if a < r.Meta.Leaves {
		out.AlphaNeighborReal = true
		out.AlphaNeighborProof = r.rimbtProof(a)
	}
	if !hasB {
		out.UpperReal = true
		out.UpperProof = r.rimbtProof(r.Meta.Leaves - 1)
		upper = out.UpperProof.Bitmap
	} else {
		var l leaf
		r.get(fmt.Sprintf("leaf/%d", b), &l)
		if l.Value == beta {
			out.UpperReal = true
			out.UpperIsExact = true
			out.UpperProof = r.rimbtProof(b)
			upper = out.UpperProof.Bitmap
		} else if b > 0 {
			out.UpperReal = true
			out.UpperProof = r.rimbtProof(b - 1)
			upper = out.UpperProof.Bitmap
			out.BetaNeighborReal = true
			out.BetaNeighborProof = r.rimbtProof(b)
		}
	}
	if r.Err() != nil {
		return rimbt.RangeResult{}, r.Err()
	}
	out.QR = r.indices(bitmap.And(bitmap.Not(lower), upper))
	if r.Err() != nil {
		return rimbt.RangeResult{}, r.Err()
	}
	return out, nil
}

func (r *Reader) indices(b bitmap.Bitmap) []uint64 {
	n := b.Count()
	if n > uint64(max(0, r.Budget.MaxBytes-r.Budget.ReadBytes))/8 {
		r.Budget.Err = errors.New("diskindex: result index array exceeds query workspace")
		return nil
	}
	if !r.charge(int(n) * 8) {
		return nil
	}
	return b.SetIndices()
}
func (r *Reader) Prove(oid uint64) (omt.Proof, bool) {
	if oid >= r.Meta.Leaves {
		return omt.Proof{}, false
	}
	var l leaf
	if !r.get(fmt.Sprintf("leaf/%d", oid), &l) {
		return omt.Proof{}, false
	}
	if l.Value < 0 || !r.charge(int(l.Value)) {
		return omt.Proof{}, false
	}
	p := omt.Proof{OID: oid, Object: make([]byte, 0, int(l.Value))}
	for i := uint64(0); len(p.Object) < int(l.Value); i++ {
		b, err := r.Store.Get(r.Ctx, r.Tree, fmt.Sprintf("object/%d/%d", oid, i))
		if err != nil {
			r.err = err
			return omt.Proof{}, false
		}
		if len(b) == 0 || len(b) > int(l.Value)-len(p.Object) {
			r.err = errors.New("diskindex: invalid object chunk")
			return omt.Proof{}, false
		}
		p.Object = append(p.Object, b...)
	}
	pos := oid
	width := r.Meta.Leaves
	for level := uint64(0); width > 1; level++ {
		start := pos / uint64(r.Meta.Fanout) * uint64(r.Meta.Fanout)
		step := omt.ProofStep{ChildIndex: int(pos - start)}
		for i := start; i < min(start+uint64(r.Meta.Fanout), width); i++ {
			if i == pos {
				continue
			}
			var n node
			if !r.get(fmt.Sprintf("node/%d/%d", level, i), &n) {
				return omt.Proof{}, false
			}
			step.SiblingHashes = append(step.SiblingHashes, n.Hash)
		}
		p.Steps = append(p.Steps, step)
		pos /= uint64(r.Meta.Fanout)
		width = (width + uint64(r.Meta.Fanout) - 1) / uint64(r.Meta.Fanout)
	}
	return p, r.Err() == nil
}
