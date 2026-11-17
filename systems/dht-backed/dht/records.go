package dht

import (
	"dhtbacked/internal/hashing"
	"dhtbacked/internal/serial"
)

// ParentNode is the DHT parent node record P_j,k = (tree_id, j, k, N_j,k,
// commit_seq) — Eq. (6)/(22), R-DHT-04.
//
// Fig. 2's Prev Hash field is deliberately not implemented (Q23).
type ParentNode struct {
	TreeID    hashing.Digest
	J         uint64
	K         uint64
	Node      hashing.Digest
	CommitSeq uint64
}

// Serialize renders P_j,k in fixed field order (Q12).
func (p ParentNode) Serialize() []byte {
	w := serial.NewWriter(serial.TypeParentNode)
	w.Digest(p.TreeID)
	w.Uint64(p.J)
	w.Uint64(p.K)
	w.Digest(p.Node)
	w.Uint64(p.CommitSeq)
	return w.Result()
}

// ParseParentNode decodes a P_j,k record.
func ParseParentNode(b []byte) (ParentNode, error) {
	var p ParentNode
	r, err := serial.NewReader(b, serial.TypeParentNode)
	if err != nil {
		return p, err
	}
	if p.TreeID, err = r.Digest(); err != nil {
		return p, err
	}
	if p.J, err = r.Uint64(); err != nil {
		return p, err
	}
	if p.K, err = r.Uint64(); err != nil {
		return p, err
	}
	if p.Node, err = r.Digest(); err != nil {
		return p, err
	}
	if p.CommitSeq, err = r.Uint64(); err != nil {
		return p, err
	}
	return p, r.End()
}

// AncestorCert is the ancestor certificate C_s,k of Eq. (5)/(6)/(23).
//
// The range field is the leaf-index range [IMin, IMax]; timestamps are never
// substituted (Q17, R-ANC-02). Certificates carry no signature field (Q4,
// R-ANC-05).
type AncestorCert struct {
	AncestorID   hashing.Digest
	TreeID       hashing.Digest
	Level        uint64 // s
	Index        uint64 // k
	AncestorHash hashing.Digest
	UpProof      []hashing.Digest
	IMin         uint64
	IMax         uint64
	CommitSeq    uint64
}

// Serialize renders C_s,k in fixed field order (Q12).
func (c AncestorCert) Serialize() []byte {
	w := serial.NewWriter(serial.TypeAncestorCer)
	w.Digest(c.AncestorID)
	w.Digest(c.TreeID)
	w.Uint64(c.Level)
	w.Uint64(c.Index)
	w.Digest(c.AncestorHash)
	w.Uint64(uint64(len(c.UpProof)))
	for i := range c.UpProof {
		w.Digest(c.UpProof[i])
	}
	w.Uint64(c.IMin)
	w.Uint64(c.IMax)
	w.Uint64(c.CommitSeq)
	return w.Result()
}

// ParseAncestorCert decodes a C_s,k record.
func ParseAncestorCert(b []byte) (AncestorCert, error) {
	var c AncestorCert
	r, err := serial.NewReader(b, serial.TypeAncestorCer)
	if err != nil {
		return c, err
	}
	if c.AncestorID, err = r.Digest(); err != nil {
		return c, err
	}
	if c.TreeID, err = r.Digest(); err != nil {
		return c, err
	}
	if c.Level, err = r.Uint64(); err != nil {
		return c, err
	}
	if c.Index, err = r.Uint64(); err != nil {
		return c, err
	}
	if c.AncestorHash, err = r.Digest(); err != nil {
		return c, err
	}
	n, err := r.Uint64()
	if err != nil {
		return c, err
	}
	if n > 64 {
		return c, serial.ErrMalformed
	}
	c.UpProof = make([]hashing.Digest, 0, n)
	for i := uint64(0); i < n; i++ {
		d, err := r.Digest()
		if err != nil {
			return c, err
		}
		c.UpProof = append(c.UpProof, d)
	}
	if c.IMin, err = r.Uint64(); err != nil {
		return c, err
	}
	if c.IMax, err = r.Uint64(); err != nil {
		return c, err
	}
	if c.CommitSeq, err = r.Uint64(); err != nil {
		return c, err
	}
	return c, r.End()
}

// RootRecord is the DHT root record (tree_id, seq, R^(e)_tau, leafCnt, tau) —
// Eq. (6), R-DHT-05.
type RootRecord struct {
	TreeID  hashing.Digest
	Seq     uint64
	Root    hashing.Digest
	LeafCnt uint64
	LogType string
}

// Serialize renders the root record in fixed field order (Q12).
func (rr RootRecord) Serialize() []byte {
	w := serial.NewWriter(serial.TypeRootRecord)
	w.Digest(rr.TreeID)
	w.Uint64(rr.Seq)
	w.Digest(rr.Root)
	w.Uint64(rr.LeafCnt)
	w.String(rr.LogType)
	return w.Result()
}

// ParseRootRecord decodes a root record.
func ParseRootRecord(b []byte) (RootRecord, error) {
	var rr RootRecord
	r, err := serial.NewReader(b, serial.TypeRootRecord)
	if err != nil {
		return rr, err
	}
	if rr.TreeID, err = r.Digest(); err != nil {
		return rr, err
	}
	if rr.Seq, err = r.Uint64(); err != nil {
		return rr, err
	}
	if rr.Root, err = r.Digest(); err != nil {
		return rr, err
	}
	if rr.LeafCnt, err = r.Uint64(); err != nil {
		return rr, err
	}
	if rr.LogType, err = r.String(); err != nil {
		return rr, err
	}
	return rr, r.End()
}

// MicroRootRecord is the micro-root record (treeID, windowID, i_min, i_max,
// R^(delta)_tau, seq) of Q22.
//
// k is not a serialized field; it is implied by IMax - IMin + 1 (Q53b). IMax
// advances on each republication as the window grows (Q53a).
type MicroRootRecord struct {
	TreeID    hashing.Digest
	WindowID  uint64
	IMin      uint64
	IMax      uint64
	MicroRoot hashing.Digest
	Seq       uint64
}

// Serialize renders the micro-root record in fixed field order (Q12).
func (m MicroRootRecord) Serialize() []byte {
	w := serial.NewWriter(serial.TypeMicroRoot)
	w.Digest(m.TreeID)
	w.Uint64(m.WindowID)
	w.Uint64(m.IMin)
	w.Uint64(m.IMax)
	w.Digest(m.MicroRoot)
	w.Uint64(m.Seq)
	return w.Result()
}

// ParseMicroRootRecord decodes a micro-root record.
func ParseMicroRootRecord(b []byte) (MicroRootRecord, error) {
	var m MicroRootRecord
	r, err := serial.NewReader(b, serial.TypeMicroRoot)
	if err != nil {
		return m, err
	}
	if m.TreeID, err = r.Digest(); err != nil {
		return m, err
	}
	if m.WindowID, err = r.Uint64(); err != nil {
		return m, err
	}
	if m.IMin, err = r.Uint64(); err != nil {
		return m, err
	}
	if m.IMax, err = r.Uint64(); err != nil {
		return m, err
	}
	if m.MicroRoot, err = r.Digest(); err != nil {
		return m, err
	}
	if m.Seq, err = r.Uint64(); err != nil {
		return m, err
	}
	return m, r.End()
}
