package main

import (
	"fmt"
	"time"

	"evseb/internal/access"
	"evseb/internal/blockchain"
	"evseb/internal/crypto"
	"evseb/internal/integrity"
	"evseb/internal/keyword"
	"evseb/internal/logstore"
	"evseb/internal/search"
	"evseb/internal/tree"
)

// chainConfig selects the BlockchainContracts backend. An empty rpcURL means
// the in-memory backend; otherwise fresh contracts are deployed on that chain.
type chainConfig struct {
	rpcURL, keyHex string
}

func (c chainConfig) name() string {
	if c.rpcURL == "" {
		return "memory"
	}
	return "hardhat@" + c.rpcURL
}

// chainFromFlag resolves -chain: "memory", or "hardhat" (from EVSEB_ETH_RPC_URL
// and EVSEB_EVM_PRIVATE_KEY_HEX).
func chainFromFlag(v string) (chainConfig, error) {
	switch v {
	case "memory":
		return chainConfig{}, nil
	case "hardhat":
		rpc, key, ok := blockchain.FromEnv()
		if !ok {
			return chainConfig{}, fmt.Errorf("-chain hardhat needs %s and %s", blockchain.EnvRPCURL, blockchain.EnvPrivateKey)
		}
		return chainConfig{rpcURL: rpc, keyHex: key}, nil
	}
	return chainConfig{}, fmt.Errorf("unknown -chain %q", v)
}

// system wires Phase 1-6 state together for the demo and the harness.
type system struct {
	keys     crypto.SystemKeys
	chain    blockchain.BlockchainContracts
	eth      *blockchain.Ethereum // non-nil on a real chain
	tree     *tree.Tree
	dht      *integrity.DHT
	storage  *logstore.Storage
	stored   *integrity.MerkleTree // stored_tree, built when R is anchored
	assigner logstore.LIDAssigner
	lids     []string // ingestion order; Merkle leaf order
	now      func() time.Time
}

// newSystem runs Phase 1 Step 1-2 (keys, contract deployment) and Phase 2
// (treeSetup).
func newSystem(bloomM, bloomK int, now func() time.Time, cc chainConfig) (*system, error) {
	keys, err := crypto.GenerateKeys()
	if err != nil {
		return nil, err
	}
	s := &system{
		keys:    keys,
		tree:    tree.TreeSetup(bloomM, bloomK),
		dht:     integrity.NewDHT(),
		storage: logstore.NewStorage(),
		now:     now,
	}
	if cc.rpcURL == "" {
		s.chain = blockchain.NewMemory(keys.Kauth, keys.Kindex, now)
	} else {
		if s.eth, err = blockchain.Deploy(cc.rpcURL, cc.keyHex, keys.Kauth, keys.Kindex); err != nil {
			return nil, err
		}
		s.chain = s.eth
	}
	return s, nil
}

func (s *system) close() {
	if s.eth != nil {
		s.eth.Close()
	}
}

// registrar is the trusted Registration/token authority.
func (s *system) registrar(validity time.Duration, verify func(u, c string) bool) *access.Registrar {
	return &access.Registrar{Chain: s.chain, Kindex: s.keys.Kindex, Kauth: s.keys.Kauth,
		Validity: validity, Now: s.now, VerifyCredentials: verify}
}

// register registers userID with permissions and returns Tokenu (Q6 ruling:
// issued at registration).
func (s *system) register(userID string, perms []string, validity time.Duration) ([]byte, error) {
	st, tok, err := s.registrar(validity, func(string, string) bool { return true }).
		RegisterUser(userID, "", perms, perms[0])
	if err != nil {
		return nil, err
	}
	if st != access.StatusRegistered {
		return nil, fmt.Errorf("register %s: %s", userID, st)
	}
	return tok, nil
}

// ingest runs Phase 3 (keyword extraction, LID, Eq. 14-15), stores the
// ciphertexts, records DHT[H(LID)] = hLID (Eq. 28) and runs Phase 4 Alg. 2.
func (s *system) ingest(lg logstore.Log) (string, error) {
	lg.LID = s.assigner.Next()
	kws := keyword.Extract(lg.Content)
	enc, err := logstore.EncryptLog(lg, s.keys.Kenc)
	if err != nil {
		return "", err
	}
	s.storage.Put(enc)
	s.dht.Put(lg.LID, integrity.HashLog(enc.CTLog))
	tree.InsertIndices(s.tree, lg.LType, lg, kws, s.keys.Ksearch)
	s.lids = append(s.lids, lg.LID)
	return lg.LID, nil
}

// anchor builds the quaternary tree over every ingested hLID from the DHT,
// stores its root R on-chain, and keeps the tree off-chain as stored_tree.
func (s *system) anchor() ([]byte, error) {
	t, err := integrity.AnchorRoot(s.lids, s.dht, s.chain)
	if err != nil {
		return nil, err
	}
	s.stored = t
	return t.Root, nil
}

func (s *system) engine() *search.Engine {
	return &search.Engine{Tree: s.tree, Chain: s.chain, Stored: s.stored, Storage: s.storage}
}

// verify runs Alg. 4.1 over candidates.
func (s *system) verify(candidates []string) ([]string, bool, error) {
	return integrity.IdentifyTamperedFiles(candidates, s.stored, s.storage, s.chain)
}

// tamper flips the last byte of a stored log ciphertext.
func (s *system) tamper(lid string) {
	e, ok := s.storage.Get(lid)
	if !ok {
		return
	}
	e.CTLog[len(e.CTLog)-1] ^= 0xff
	s.storage.Put(e)
}
