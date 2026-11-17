package mktree

import (
	"errors"
	"fmt"
	"sort"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
)

// Config fixes the partitioner's four configurable quantities.
type Config struct {
	// EpochCapacity is the count-based epoch boundary N_e: the number of
	// accepted logs after which the epoch closes (Q23). Default 1,000.
	EpochCapacity uint64
	// Stride is s (Q25). Default 8 for the default 1,024-leaf tree.
	Stride uint64
	// Window is the tumbling micro-root window W (Q21, Q54). Default 1,024.
	Window uint64
	// LogTypes is the fixed set of log types, in lexicographic order by
	// canonical UTF-8 log_type (Q26, Q29).
	LogTypes []string
}

// DefaultConfig returns the default configuration: epoch 1,000, s = 8,
// W = 1,024, and the five evaluation log types in lexicographic order.
func DefaultConfig() Config {
	return Config{
		EpochCapacity: DefaultEpochCapacity,
		Stride:        DefaultStride,
		Window:        DefaultWindow,
		LogTypes:      []string{"access", "api", "auth", "error", "security"},
	}
}

// RootOfRoots computes R^(e)_agg = H(R_tau1 || ... || R_taum) with no
// domain-separation tag, exactly as Eq. (7)/(24)/(30) is written (R-EPO-01).
//
// The caller supplies the per-type roots already ordered lexicographically by
// canonical UTF-8 log_type (Q26).
func RootOfRoots(roots []hashing.Digest) hashing.Digest {
	parts := make([][]byte, 0, len(roots))
	for i := range roots {
		parts = append(parts, roots[i][:])
	}
	return hashing.H(parts...)
}

// Partitioner maintains the per-type MK-Trees of one epoch, publishes proof
// state to the DHT, and produces the epoch's root-of-roots (R-SYS-05).
//
// It is single-goroutine: ingestion and verification never interleave (Q10,
// Q32, Q52).
type Partitioner struct {
	cfg   Config
	store *dht.DHT

	epoch     uint64
	seq       uint64 // global, monotonically increasing, first update uses 1 (Q23)
	trees     map[string]*Tree
	accepted  uint64
	finalized bool
}

// Errors returned by the partitioner.
var (
	ErrEpochClosed  = errors.New("mktree: the epoch is finalized")
	ErrUnknownType  = errors.New("mktree: log type is not configured")
	ErrEpochNotFull = errors.New("mktree: the epoch has not reached its capacity")
)

// NewPartitioner starts epoch 1. One MK-Tree is created per configured log
// type, each pre-sized to the epoch capacity and padded from the beginning
// (Q24). seq begins at 0 and is incremented to 1 by the first accepted update
// (Q23).
func NewPartitioner(cfg Config, store *dht.DHT) (*Partitioner, error) {
	if store == nil {
		return nil, errors.New("mktree: a DHT is required")
	}
	if len(cfg.LogTypes) == 0 {
		return nil, errors.New("mktree: at least one log type is required")
	}
	if cfg.Window == 0 {
		return nil, errors.New("mktree: window W must be positive")
	}
	// Q26 fixes the aggregation order as lexicographic by canonical UTF-8
	// log_type. Derive that order here rather than trusting the order the
	// caller supplied: Finalize walks cfg.LogTypes to build R^(e)_agg while the
	// verifier's anchor reconciliation sorts independently, so an unsorted
	// configuration would commit one aggregate and verify against another.
	cfg.LogTypes = append([]string(nil), cfg.LogTypes...)
	sort.Strings(cfg.LogTypes)

	p := &Partitioner{cfg: cfg, store: store, epoch: 1}
	if err := p.openEpoch(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Partitioner) openEpoch() error {
	p.trees = make(map[string]*Tree, len(p.cfg.LogTypes))
	for _, lt := range p.cfg.LogTypes {
		t, err := NewTree(p.epoch, lt, p.cfg.EpochCapacity, p.cfg.Stride)
		if err != nil {
			return err
		}
		p.trees[lt] = t
	}
	p.accepted = 0
	p.finalized = false
	return nil
}

// Epoch returns the current epoch number.
func (p *Partitioner) Epoch() uint64 { return p.epoch }

// Seq returns the current global sequence value.
func (p *Partitioner) Seq() uint64 { return p.seq }

// Accepted returns the number of records admitted to the current epoch.
func (p *Partitioner) Accepted() uint64 { return p.accepted }

// Finalized reports whether the current epoch has been finalized.
func (p *Partitioner) Finalized() bool { return p.finalized }

// Config returns the partitioner's configuration.
func (p *Partitioner) Config() Config { return p.cfg }

// Tree returns the MK-Tree for a log type in the current epoch.
func (p *Partitioner) Tree(logType string) (*Tree, error) {
	t, ok := p.trees[logType]
	if !ok {
		return nil, ErrUnknownType
	}
	return t, nil
}

// Insert admits one verified leaf digest into the (epoch, logType) tree.
//
// It advances the global seq once (Q23), places the digest in the next dummy
// position and recomputes only the affected path (Q24), publishes the
// recomputed internal nodes below the ancestor level as P_j,k records
// (R-DHT-04), publishes the covering ancestor certificate and the root record,
// and republishes the provisional micro-root for the affected window (Q53a).
//
// It returns the assigned zero-based leaf index (Q44).
func (p *Partitioner) Insert(logType string, d hashing.Digest) (uint64, error) {
	if p.finalized {
		return 0, ErrEpochClosed
	}
	if p.accepted >= p.cfg.EpochCapacity {
		return 0, ErrEpochClosed
	}
	t, ok := p.trees[logType]
	if !ok {
		return 0, ErrUnknownType
	}
	leafIndex, affected, err := t.Insert(d)
	if err != nil {
		return 0, err
	}
	p.seq++
	p.accepted++

	// Only nodes at levels j > s are published as P_j,k; levels j < s are
	// covered by up-proofs (R-DHT-04 acceptance).
	for _, c := range affected {
		if c.J <= p.cfg.Stride {
			continue
		}
		n, err := t.Node(c.J, c.K)
		if err != nil {
			return 0, err
		}
		p.store.PutParent(dht.ParentNode{TreeID: t.TreeID(), J: c.J, K: c.K, Node: n, CommitSeq: p.seq})
	}

	k := t.CoveringAncestor(leafIndex)
	cert, err := t.Certificate(k, p.seq)
	if err != nil {
		return 0, err
	}
	p.store.PutCert(cert)
	p.publishRoot(t)
	if err := p.publishMicroRoot(t, WindowID(leafIndex, p.cfg.Window)); err != nil {
		return 0, err
	}
	return leafIndex, nil
}

func (p *Partitioner) publishRoot(t *Tree) {
	p.store.PutRoot(dht.RootRecord{
		TreeID:  t.TreeID(),
		Seq:     p.seq,
		Root:    t.Root(),
		LeafCnt: t.LeafCount(),
		LogType: t.LogType(),
	})
}

// publishMicroRoot recomputes and republishes the micro-root for one window at
// its stable Key_M (Q45 overwrite semantics, Q53a).
//
// Only digests of real records participate; dummy padding leaves never enter a
// micro-root (Q53b). i_min is fixed for the window and i_max advances with
// each republication, so it always names the range actually hashed.
func (p *Partitioner) publishMicroRoot(t *Tree, windowID uint64) error {
	iMin := WindowStart(windowID, p.cfg.Window)
	if iMin >= t.LeafCount() {
		return nil
	}
	iMax := iMin + p.cfg.Window - 1
	if last := t.LeafCount() - 1; iMax > last {
		iMax = last
	}
	digests := make([]hashing.Digest, 0, iMax-iMin+1)
	for i := iMin; i <= iMax; i++ {
		n, err := t.Node(t.Levels(), i)
		if err != nil {
			return err
		}
		digests = append(digests, n)
	}
	p.store.PutMicroRoot(dht.MicroRootRecord{
		TreeID:    t.TreeID(),
		WindowID:  windowID,
		IMin:      iMin,
		IMax:      iMax,
		MicroRoot: MicroRoot(digests),
		Seq:       p.seq,
	})
	return nil
}

// Full reports whether the epoch has reached its count-based boundary (Q23).
func (p *Partitioner) Full() bool { return p.accepted >= p.cfg.EpochCapacity }

// Finalize closes the epoch: it republishes the complete final DHT state for
// every tree in the epoch and returns the epoch's per-type roots, in
// lexicographic log-type order, together with R^(e)_agg.
//
// After finalization the epoch's DHT records are no longer modified (Q45).
func (p *Partitioner) Finalize() (roots []hashing.Digest, agg hashing.Digest, err error) {
	if p.finalized {
		return nil, hashing.Digest{}, ErrEpochClosed
	}
	for _, lt := range p.cfg.LogTypes {
		t := p.trees[lt]
		if err := p.publishTreeState(t); err != nil {
			return nil, hashing.Digest{}, err
		}
		roots = append(roots, t.Root())
	}
	p.finalized = true
	return roots, RootOfRoots(roots), nil
}

// publishTreeState writes the tree's complete final state to the DHT: every
// P_j,k at a level below the ancestor level, every ancestor certificate, the
// root record, and every window's micro-root.
func (p *Partitioner) publishTreeState(t *Tree) error {
	for j := p.cfg.Stride + 1; j <= t.Levels(); j++ {
		for k := uint64(0); k < uint64(1)<<j; k++ {
			n, err := t.Node(j, k)
			if err != nil {
				return err
			}
			p.store.PutParent(dht.ParentNode{TreeID: t.TreeID(), J: j, K: k, Node: n, CommitSeq: p.seq})
		}
	}
	for k := uint64(0); k < t.AncestorCount(); k++ {
		cert, err := t.Certificate(k, p.seq)
		if err != nil {
			return err
		}
		p.store.PutCert(cert)
	}
	p.publishRoot(t)
	if t.LeafCount() > 0 {
		last := WindowID(t.LeafCount()-1, p.cfg.Window)
		for w := uint64(0); w <= last; w++ {
			if err := p.publishMicroRoot(t, w); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetEpoch restarts the partitioner at a given epoch number before any record
// is admitted. It exists because the anchoring contract is append-only (Q14):
// a benchmark run against a live chain must start above the epochs already
// anchored there.
func (p *Partitioner) SetEpoch(epoch uint64) error {
	if p.accepted > 0 || p.seq > 0 {
		return errors.New("mktree: the epoch may only be set before the first update")
	}
	p.epoch = epoch
	return p.openEpoch()
}

// NextEpoch advances to the following epoch, creating a fresh set of pre-sized
// trees. The previous epoch must already be finalized.
func (p *Partitioner) NextEpoch() error {
	if !p.finalized {
		return fmt.Errorf("mktree: epoch %d is not finalized", p.epoch)
	}
	p.epoch++
	return p.openEpoch()
}
