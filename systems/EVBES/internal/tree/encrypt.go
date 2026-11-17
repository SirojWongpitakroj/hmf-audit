package tree

import (
	"encoding/binary"
	"errors"

	"evseb/internal/bloom"
	"evseb/internal/crypto"
	"evseb/internal/index"
)

// EncryptedIndex is (CT_T, CT_BF, CT_Inv) of Eq. 17.
type EncryptedIndex struct {
	CTT, CTBF, CTInv []byte
}

// Encrypt is Eq. 17: Enc(T, BF, Inv, Kindex) = (CT_T, CT_BF, CT_Inv).
//
// The paper gives no pre-encryption format. Each part is a deterministic
// binary encoding (uint32 big-endian counts and lengths; primary nodes,
// sub-nodes and index keys in ascending order; Go maps are never serialized
// directly):
//
//	T   = BloomM, BloomK, #primary, { LType, #sub, { subKey } }
//	BF  = for each sub-node in T order: M, K, #bytes, packed bits (LSB first)
//	Inv = for each primary in T order: INL, then each sub-node's In_ij,
//	      where an index is #entries, { key, #LIDs, { LID } }
func Encrypt(t *Tree, kindex [32]byte) (EncryptedIndex, error) {
	var tb, bb, ib []byte
	tb = u32(u32(u32(tb, t.BloomM), t.BloomK), len(t.PrimaryNodes))
	for _, lt := range t.sortedLTypes() {
		n := t.PrimaryNodes[lt]
		keys := n.sortedSubKeys()
		tb = u32(str(tb, lt), len(keys))
		ib = encIndex(ib, n.InvertedIndex)
		for _, k := range keys {
			s := n.SubNodes[k]
			tb = str(tb, k)
			bb = encBloom(bb, s.BloomFilter)
			ib = encIndex(ib, s.InvertedIndex)
		}
	}
	var out EncryptedIndex
	var err error
	if out.CTT, err = crypto.EncAES(tb, kindex); err != nil {
		return out, err
	}
	if out.CTBF, err = crypto.EncAES(bb, kindex); err != nil {
		return out, err
	}
	out.CTInv, err = crypto.EncAES(ib, kindex)
	return out, err
}

// Decrypt reverses Encrypt (the "decrypted_index" Alg. 2 line 4 works on).
func Decrypt(e EncryptedIndex, kindex [32]byte) (*Tree, error) {
	tb, err := crypto.DecAES(e.CTT, kindex)
	if err != nil {
		return nil, err
	}
	bb, err := crypto.DecAES(e.CTBF, kindex)
	if err != nil {
		return nil, err
	}
	ib, err := crypto.DecAES(e.CTInv, kindex)
	if err != nil {
		return nil, err
	}
	rt, rb, ri := &reader{b: tb}, &reader{b: bb}, &reader{b: ib}
	t := &Tree{PrimaryNodes: map[string]*PrimaryNode{}, BloomM: rt.u32(), BloomK: rt.u32()}
	np := rt.u32()
	for range np {
		n := createPrimaryNode(rt.str())
		n.InvertedIndex = decIndex(ri)
		ns := rt.u32()
		for range ns {
			k := rt.str()
			n.SubNodes[k] = &SubNode{Key: k, BloomFilter: decBloom(rb), InvertedIndex: decIndex(ri)}
		}
		t.PrimaryNodes[n.LType] = n
	}
	if rt.bad || rb.bad || ri.bad || len(rt.b)+len(rb.b)+len(ri.b) != 0 {
		return nil, errors.New("tree: malformed index encoding")
	}
	return t, nil
}

func encBloom(b []byte, f *bloom.BloomFilter) []byte {
	packed := make([]byte, (f.M+7)/8)
	for i, bit := range f.Bits {
		if bit {
			packed[i/8] |= 1 << (i % 8)
		}
	}
	b = u32(u32(u32(b, f.M), f.K), len(packed))
	return append(b, packed...)
}

func decBloom(r *reader) *bloom.BloomFilter {
	m, k := r.u32(), r.u32()
	packed := r.bytes(r.u32())
	if r.bad || m < 1 || k < 1 || len(packed) != (m+7)/8 {
		r.bad = true
		return &bloom.BloomFilter{}
	}
	f := bloom.New(m, k)
	for i := range f.Bits {
		f.Bits[i] = packed[i/8]&(1<<(i%8)) != 0
	}
	return f
}

func encIndex(b []byte, ix *index.InvertedIndex) []byte {
	keys := ix.SortedKeys()
	b = u32(b, len(keys))
	for _, k := range keys {
		lids := ix.Entries[k]
		b = u32(str(b, k), len(lids))
		for _, l := range lids {
			b = str(b, l)
		}
	}
	return b
}

func decIndex(r *reader) *index.InvertedIndex {
	ix := index.New()
	n := r.u32()
	for range n {
		if r.bad {
			break
		}
		k := r.str()
		m := r.u32()
		lids := make([]string, 0, min(m, 1024))
		for range m {
			if r.bad {
				break
			}
			lids = append(lids, r.str())
		}
		ix.Entries[k] = lids
	}
	return ix
}

func u32(b []byte, v int) []byte    { return binary.BigEndian.AppendUint32(b, uint32(v)) }
func str(b []byte, s string) []byte { return append(u32(b, len(s)), s...) }

type reader struct {
	b   []byte
	bad bool
}

func (r *reader) u32() int {
	if r.bad || len(r.b) < 4 {
		r.bad = true
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return int(v)
}

func (r *reader) bytes(n int) []byte {
	if r.bad || n < 0 || n > len(r.b) {
		r.bad = true
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) str() string { return string(r.bytes(r.u32())) }
