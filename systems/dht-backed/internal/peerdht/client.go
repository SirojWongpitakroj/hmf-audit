package peerdht

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/verifier"
)

type Client struct {
	Config                                        Config
	Ctx                                           context.Context
	http                                          *http.Client
	lookups, hops, reads, hits, misses, ancestors atomic.Uint64
	proofBytes                                    atomic.Uint64
}

func NewClient(ctx context.Context, c Config) (*Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Client{Config: c, Ctx: ctx, http: newHTTP(c)}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) Put(key hashing.Digest, value []byte) error {
	if len(value) > c.Config.Limits.MaxValueBytes {
		return errors.New("peerdht: publication value budget exceeded")
	}
	return post(c.Ctx, c.http, c.Config.Peers[Owner(key)]+"/put", Publication{Key: key, Value: value, Overlay: c.Config.Overlay}, c.Config.WriteSecret, c.Config.Limits.MaxRequestBytes, nil)
}
func (c *Client) lookup(tok *auth.Token, r verifier.ProofRequest, micro bool) ([]byte, error) {
	key, err := expected(r, micro)
	if err != nil || key != r.Key {
		return nil, dht.ErrKeyMismatch
	}
	if err = auth.Verify(c.Config.CA, tok, time.Now(), r.Epoch, r.LogType); err != nil {
		return nil, err
	}
	var out Reply
	err = post(c.Ctx, c.http, c.Config.Peers[0]+"/lookup", Envelope{Token: tok, Request: r, Micro: micro, Overlay: c.Config.Overlay}, "", c.Config.Limits.MaxRequestBytes, &out)
	if err != nil {
		return nil, err
	}
	if len(out.Value) > c.Config.Limits.MaxValueBytes || out.Hops < 0 || out.Hops > 2 {
		return nil, errors.New("peerdht: malformed or oversized reply")
	}
	c.lookups.Add(1)
	c.proofBytes.Add(uint64(len(out.Value)))
	c.hops.Add(uint64(out.Hops))
	c.reads.Add(out.StorageReads)
	if out.CacheHit {
		c.hits.Add(1)
	} else {
		c.misses.Add(1)
	}
	if !micro && (r.Kind == verifier.ProofParent || r.Kind == verifier.ProofCert) {
		c.ancestors.Add(1)
	}
	return out.Value, nil
}
func (c *Client) GetParent(t *auth.Token, r dht.ParentRequest) ([]byte, error) {
	return c.lookup(t, verifier.ProofRequest{Kind: verifier.ProofParent, Epoch: r.Epoch, LogType: r.LogType, A: r.J, B: r.K, Key: r.Key}, false)
}
func (c *Client) GetCert(t *auth.Token, r dht.CertRequest) ([]byte, error) {
	return c.lookup(t, verifier.ProofRequest{Kind: verifier.ProofCert, Epoch: r.Epoch, LogType: r.LogType, A: r.S, B: r.K, Key: r.Key}, false)
}
func (c *Client) GetRoot(t *auth.Token, r dht.RootRequest) ([]byte, error) {
	return c.lookup(t, verifier.ProofRequest{Kind: verifier.ProofRoot, Epoch: r.Epoch, LogType: r.LogType, Key: r.Key}, false)
}
func (c *Client) GetMicroRoot(t *auth.Token, r dht.MicroRootRequest) ([]byte, error) {
	return c.lookup(t, verifier.ProofRequest{Epoch: r.Epoch, LogType: r.LogType, A: r.WindowID, Key: r.Key}, true)
}

func (c *Client) GetMany(tok *auth.Token, requests []verifier.ProofRequest) (map[hashing.Digest][]byte, error) {
	l := c.Config.Limits
	if len(requests) > l.MaxRequests {
		return nil, errors.New("peerdht: proof request count limit exceeded")
	}
	keys := make([]verifier.ProofRequest, 0, len(requests))
	seen := make(map[hashing.Digest]bool)
	used := 0
	for _, r := range requests {
		key, err := expected(r, false)
		if err != nil || key != r.Key {
			return nil, dht.ErrKeyMismatch
		}
		if !seen[key] {
			used += len(r.LogType) + 256
			if used > l.AncestorMapBytes {
				return nil, errors.New("peerdht: request map byte limit exceeded")
			}
			seen[key] = true
			keys = append(keys, r)
		}
	}
	// Queue capacity is a fixed byte budget, not len(all requested keys).
	jobs := make(chan verifier.ProofRequest, min(l.QueueBytes/256, len(keys)))
	out := make(map[hashing.Digest][]byte)
	var mu sync.Mutex
	var first error
	total := used
	var wg sync.WaitGroup
	workers := min(l.Workers, len(keys))
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for r := range jobs {
				mu.Lock()
				failed := first != nil
				mu.Unlock()
				if failed {
					continue
				}
				value, err := c.lookup(tok, r, false)
				mu.Lock()
				if err != nil {
					if first == nil {
						first = err
					}
				} else {
					cost := len(value) + 256
					if cost > l.ProofBytes-total || cost > l.AncestorMapBytes-total {
						if first == nil {
							first = errors.New("peerdht: proof/result map byte limit exceeded")
						}
					} else {
						total += cost
						out[r.Key] = value
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, r := range keys {
		jobs <- r
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, nil
}
func (c *Client) Stats() Stats {
	return Stats{Lookups: c.lookups.Load(), Hops: c.hops.Load(), StorageReads: c.reads.Load(), CacheHits: c.hits.Load(), CacheMisses: c.misses.Load(), AncestorRecords: c.ancestors.Load(), ProofBytes: c.proofBytes.Load()}
}
