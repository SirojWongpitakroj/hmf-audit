package peerdht

import (
	"bytes"
	contract "canonicalingestion"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/verifier"
)

func Owner(key hashing.Digest) int {
	h := sha256.Sum256(key[:])
	return int(binary.BigEndian.Uint64(h[:8]) % 4)
}

type Config struct {
	Node        int
	Overlay     string
	Peers       []string
	CA          ed25519.PublicKey
	WriteSecret string
	Limits      Limits
	Timeout     time.Duration
}

func (c Config) Validate() error {
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	if c.Node < 0 || c.Node >= 4 || len(c.Peers) != 4 || len(c.CA) != ed25519.PublicKeySize || c.Overlay == "" || c.WriteSecret == "" || c.Timeout <= 0 {
		return errors.New("peerdht: four endpoints, node, overlay, authority, write credential and timeout required")
	}
	for i, p := range c.Peers {
		if !strings.HasPrefix(p, "http://") && !strings.HasPrefix(p, "https://") {
			return errors.New("peerdht: invalid endpoint")
		}
		for j := 0; j < i; j++ {
			if p == c.Peers[j] {
				return errors.New("peerdht: peer endpoints must be distinct")
			}
		}
	}
	return nil
}

type Envelope struct {
	Token   *auth.Token
	Request verifier.ProofRequest
	Micro   bool
	Hops    int
	Overlay string
}
type Reply struct {
	Value        []byte
	Hops         int
	CacheHit     bool
	StorageReads uint64
}
type Publication struct {
	Key     hashing.Digest
	Value   []byte
	Overlay string
}
type Peer struct {
	Config        Config
	Store         *LocalStore
	cache         *cache
	http          *http.Client
	active        chan struct{}
	lookups, hops atomic.Uint64
}

func NewPeer(ctx context.Context, c Config, store *LocalStore) (*Peer, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if store == nil || store.Node != c.Node || store.Overlay != c.Overlay {
		return nil, errors.New("peerdht: node-local store identity mismatch")
	}
	identity := sha256.Sum256(append([]byte(c.Overlay+"/four-peer-v1/"), c.CA...))
	if err := store.Migrate(ctx, identity[:]); err != nil {
		return nil, err
	}
	return &Peer{Config: c, Store: store, cache: newCache(c.Limits), http: newHTTP(c), active: make(chan struct{}, c.Limits.Workers)}, nil
}
func newHTTP(c Config) *http.Client {
	return &http.Client{Timeout: c.Timeout, Transport: &http.Transport{MaxIdleConns: 4 * c.Limits.Workers, MaxIdleConnsPerHost: c.Limits.Workers, MaxConnsPerHost: c.Limits.Workers, ResponseHeaderTimeout: c.Timeout}}
}
func expected(r verifier.ProofRequest, micro bool) (hashing.Digest, error) {
	tree := dht.TreeID(r.Epoch, r.LogType)
	if micro {
		return dht.KeyM(tree, r.A), nil
	}
	switch r.Kind {
	case verifier.ProofParent:
		return dht.KeyP(tree, r.A, r.B), nil
	case verifier.ProofCert:
		return dht.KeyC(tree, r.A, r.B), nil
	case verifier.ProofRoot:
		return dht.KeyR(tree), nil
	}
	return hashing.Digest{}, errors.New("unknown proof kind")
}
func decodeBody(w http.ResponseWriter, r *http.Request, max int, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, int64(max))
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("trailing request body")
	}
	return nil
}
func (p *Peer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case p.active <- struct{}{}:
		defer func() { <-p.active }()
	default:
		http.Error(w, "peer request limit", http.StatusTooManyRequests)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/stats" {
		json.NewEncoder(w).Encode(p.Stats())
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		json.NewEncoder(w).Encode(struct {
			Node    int
			Overlay string
		}{p.Config.Node, p.Config.Overlay})
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/put-many" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+p.Config.WriteSecret)) != 1 {
			http.Error(w, "publication refused", http.StatusForbidden)
			return
		}
		var body PublicationBatch
		if err := decodeBody(w, r, p.Config.Limits.MaxRequestBytes, &body); err != nil || body.Overlay != p.Config.Overlay || len(body.Entries) > p.Config.Limits.MaxRequests {
			http.Error(w, "invalid publication batch", http.StatusBadRequest)
			return
		}
		if err := p.Store.PutMany(r.Context(), body.Entries); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/put" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+p.Config.WriteSecret)) != 1 {
			http.Error(w, "publication refused", http.StatusForbidden)
			return
		}
		var body Publication
		if err := decodeBody(w, r, p.Config.Limits.MaxRequestBytes, &body); err != nil {
			http.Error(w, "invalid publication", http.StatusBadRequest)
			return
		}
		if body.Overlay != p.Config.Overlay || Owner(body.Key) != p.Config.Node {
			http.Error(w, "wrong owner/overlay", http.StatusConflict)
			return
		}
		if err := p.Store.Put(r.Context(), body.Key, body.Value); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/lookup" {
		http.NotFound(w, r)
		return
	}
	var e Envelope
	if err := decodeBody(w, r, p.Config.Limits.MaxRequestBytes, &e); err != nil {
		http.Error(w, "invalid lookup", http.StatusBadRequest)
		return
	}
	key, err := expected(e.Request, e.Micro)
	if err != nil || key != e.Request.Key || e.Overlay != p.Config.Overlay || e.Hops < 0 || e.Hops > 2 {
		http.Error(w, "invalid key/envelope", http.StatusBadRequest)
		return
	}
	if err = auth.Verify(p.Config.CA, e.Token, time.Now(), e.Request.Epoch, e.Request.LogType); err != nil {
		http.Error(w, "authorization refused", http.StatusForbidden)
		return
	}
	target := Owner(key)
	p.lookups.Add(1)
	if target != p.Config.Node {
		if e.Hops == 2 {
			http.Error(w, "routing limit", http.StatusBadGateway)
			return
		}
		difference := p.Config.Node ^ target
		level := 0
		if difference&2 != 0 {
			level = 1
		}
		next := p.Config.Node ^ (1 << level)
		e.Hops++
		p.hops.Add(1)
		var reply Reply
		if err = post(r.Context(), p.http, p.Config.Peers[next]+"/lookup", e, "", p.Config.Limits.MaxRequestBytes, &reply); err != nil {
			http.Error(w, "responsible peer unavailable", http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(reply)
		return
	}
	value, hit := p.cache.get(key)
	reads := uint64(0)
	if !hit {
		value, err = p.Store.Get(r.Context(), key)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "persistent lookup failed", http.StatusInternalServerError)
			return
		}
		reads = 1
		p.cache.put(key, value)
	}
	json.NewEncoder(w).Encode(Reply{Value: value, Hops: e.Hops, CacheHit: hit, StorageReads: reads})
}
func (p *Peer) Stats() Stats {
	s := p.cache.stats()
	s.Memory = contract.ReadMemory()
	s.StorageReads = p.Store.Reads.Load()
	s.StorageWrites = p.Store.Writes.Load()
	s.Lookups = p.lookups.Load()
	s.Hops = p.hops.Load()
	return s
}
func post(ctx context.Context, client *http.Client, url string, body any, secret string, max int, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if len(b) > max {
		return errors.New("peerdht: request byte limit exceeded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(max)+1))
	if err != nil {
		return err
	}
	if len(data) > max {
		return errors.New("peerdht: response byte limit exceeded")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("peerdht: request returned status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
