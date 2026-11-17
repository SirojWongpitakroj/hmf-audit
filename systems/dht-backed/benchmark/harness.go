package benchmark

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/ingestverifier"
	"dhtbacked/internal/clock"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

// Options describes one assembled system.
type Options struct {
	// Config is the partitioner configuration: epoch capacity, stride s,
	// window W and the log-type set.
	Config mktree.Config
	// AEADKey is the single system-wide 256-bit AEAD key (Q2).
	AEADKey []byte
	// KeyDir holds the five producers' persistent Ed25519 key pairs (Q47).
	KeyDir string
	// RegistryPath is where the deterministic producer registry is written and
	// then loaded from before ingestion (Q5, Q46).
	RegistryPath string
	// CADir holds the CA key pair files (Q46).
	CADir string
	// Store is the data lake.
	Store storage.Store
	// Chain is the anchoring chain.
	Chain Chain
	// Clock is the injectable clock (Q40).
	Clock clock.Clock
}

// System is the in-process simulator used by unit tests and the original
// micro experiments. The fair persistence runner uses PostgreSQL for tuples
// and Cassandra-backed DHT proof records instead.
type System struct {
	Options   Options
	Pipeline  *Pipeline
	DHT       *dht.DHT
	CA        *auth.CA
	Verifier  *verifier.Verifier
	Producers []producer.KeyPair
	Registry  *ingestverifier.Registry
}

// NewSystem assembles the simulator in the Q46 startup order: load CA and
// producer keys, populate and load the registry, then start the pipeline.
func NewSystem(opts Options) (*System, error) {
	if opts.Store == nil || opts.Chain == nil || opts.Clock == nil {
		return nil, errors.New("benchmark: store, chain and clock are required")
	}
	enc, err := encryptor.New(opts.AEADKey)
	if err != nil {
		return nil, err
	}
	keys, err := producer.LoadOrCreateKeys(opts.KeyDir)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]ed25519.PublicKey, len(keys))
	for _, kp := range keys {
		entries[kp.ID] = kp.Public
	}
	if err := ingestverifier.WriteRegistry(opts.RegistryPath, entries); err != nil {
		return nil, err
	}
	reg, err := ingestverifier.LoadRegistry(opts.RegistryPath)
	if err != nil {
		return nil, err
	}
	caPriv, _, err := auth.LoadOrCreateCAKeys(filepath.Join(opts.CADir, "ca.sk"), filepath.Join(opts.CADir, "ca.pk"))
	if err != nil {
		return nil, err
	}
	ca, err := auth.NewCA(caPriv, opts.Clock)
	if err != nil {
		return nil, err
	}
	store := dht.New(ca.PublicKey(), opts.Clock)
	part, err := mktree.NewPartitioner(opts.Config, store)
	if err != nil {
		return nil, err
	}
	return &System{
		Options:   opts,
		Pipeline:  NewPipeline(enc, keys, reg, opts.Store, part, opts.Chain),
		DHT:       store,
		CA:        ca,
		Verifier:  verifier.New(opts.Store, store, opts.Chain, opts.Config),
		Producers: keys,
		Registry:  reg,
	}, nil
}

// Token issues an audit token covering every configured log type for an epoch.
func (s *System) Token(auditorID string, epoch uint64) auth.Token {
	return s.CA.Issue(auditorID, epoch, s.Options.Config.LogTypes)
}
