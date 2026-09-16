// Package logcache implements LogCacheQueue, psi_{pi,li} (DATA-001..007;
// Eq. (1), §3.2(1) p. 3288).
//
// psi_{pi,li} is an ordered sequence, in record (xi) order, of exactly theta
// record-hash digests, with duplicates kept — no deduplication (GAP-04(a)).
// theta is required configuration with no global default (GAP-55); a store
// created with theta <= 0 refuses to run.
//
// Every server retains the LogCacheQueue of every other server in the domain
// (DATA-006, domain-wide scope per GAP-10), but retention is bounded to the
// current batch only (GAP-13(a), PERSIST-002): once a batch fills, the
// completed digests are handed to the caller (LSG stage 2) and the queue for
// that server resets for the next batch. This package must not contain ring
// buffers, eviction policies or a database (SRS Section 26, item 4).
package logcache

import (
	"errors"

	"delia/hash"
)

// ServerID identifies a server within the domain. Any stable, comparable
// identifier works here (e.g. the server's position in the P order frozen at
// Init); this package attaches no meaning to the value beyond using it as a
// map key.
type ServerID string

// Store holds, per server, the record-hash digests of that server's current
// (in-progress or just-completed) batch, in xi order.
type Store struct {
	theta   int
	batches map[ServerID][]hash.Digest
}

// New creates a Store for the given theta (DATA-007/GAP-55: required
// configuration, no default). It returns an error if theta <= 0.
func New(theta int) (*Store, error) {
	if theta <= 0 {
		return nil, errors.New("logcache: theta must be configured and > 0 (GAP-55: no global default)")
	}
	return &Store{theta: theta, batches: make(map[ServerID][]hash.Digest)}, nil
}

// Theta returns the configured batch size.
func (s *Store) Theta() int { return s.theta }

// Append adds digest (H(delta_{server,xi})) as the next record hash for
// server, in the order it is called (record order, xi order). Duplicate
// digests are kept, never deduplicated (GAP-04(a)).
//
// If this append completes the batch (theta records reached), Append returns
// full=true and the completed batch (exactly theta digests, in order); the
// store's current batch for that server is then reset so the next Append
// starts batch li+1.
func (s *Store) Append(server ServerID, digest hash.Digest) (full bool, batch []hash.Digest) {
	cur := append(s.batches[server], digest)
	if len(cur) == s.theta {
		completed := make([]hash.Digest, s.theta)
		copy(completed, cur)
		s.batches[server] = nil
		return true, completed
	}
	s.batches[server] = cur
	return false, nil
}

// Current returns the server's current (partial or, transiently, exactly
// theta before the next Append call triggers a reset) batch, in order. The
// returned slice is a copy; callers must not mutate the Store through it.
func (s *Store) Current(server ServerID) []hash.Digest {
	cur := s.batches[server]
	out := make([]hash.Digest, len(cur))
	copy(out, cur)
	return out
}

// SetCurrent replaces the stored current batch for server outright. This is
// how a receiver's own locally-maintained psi for another server is
// populated once LSG stage 1 (the record-hash broadcast, blocked by
// GAP-05/OPEN) delivers hashes; test harnesses use it directly to stand in
// for an already-populated psi (see the lsg package).
func (s *Store) SetCurrent(server ServerID, batch []hash.Digest) {
	cur := make([]hash.Digest, len(batch))
	copy(cur, batch)
	s.batches[server] = cur
}

// RecordHash computes H(delta_{pi,xi}) for one log record, per Eq. (1) and
// the frozen GAP-03(a) ruling: SHA-256 over the record's raw UTF-8 line
// bytes, excluding line-ending bytes. Callers are responsible for supplying
// line bytes with any line terminator already stripped (e.g. via
// bufio.Scanner's default ScanLines split function, which strips \n and
// \r\n).
func RecordHash(line []byte) hash.Digest {
	return hash.H(line)
}
