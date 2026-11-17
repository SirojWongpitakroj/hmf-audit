// Package producer holds the log producers: their persistent Ed25519 key
// material and the deterministic synthetic log generator used for evaluation.
package producer

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// The five fixed producers, one per evaluation log type (Q47). The slice is in
// lexicographic order and is never reordered.
var IDs = []string{
	"producer-access",
	"producer-api",
	"producer-auth",
	"producer-error",
	"producer-security",
}

// LogTypes are the five evaluation log types (Q29), in the lexicographic order
// used for root-of-roots aggregation (Q26).
var LogTypes = []string{"access", "api", "auth", "error", "security"}

// IDForLogType returns the producer deterministically assigned to a log type
// (Q47). Each log is assigned to the producer corresponding to its log_type.
func IDForLogType(logType string) string { return "producer-" + logType }

// Key file encoding is deterministic binary — raw Ed25519 key bytes, not X.509
// and not JWT (Q4). Private keys are 64 bytes, public keys 32 bytes (Q3).
const (
	privateKeySuffix = ".key"
	publicKeySuffix  = ".pub"
)

// KeyPair is one producer's persistent Ed25519 key pair.
type KeyPair struct {
	ID      string
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// LoadOrCreateKeys loads the five producers' persistent key pairs from dir,
// generating any that do not yet exist with crypto/rand and persisting them so
// they are reused across runs (Q47).
//
// An existing key file is never replaced: this function never silently
// regenerates key material (Q46).
func LoadOrCreateKeys(dir string) ([]KeyPair, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	out := make([]KeyPair, 0, len(IDs))
	for _, id := range IDs {
		kp, err := loadOrCreate(dir, id)
		if err != nil {
			return nil, err
		}
		out = append(out, kp)
	}
	return out, nil
}

// LoadKeys loads the five producers' key pairs, failing if any is absent.
func LoadKeys(dir string) ([]KeyPair, error) {
	out := make([]KeyPair, 0, len(IDs))
	for _, id := range IDs {
		kp, err := loadKeyPair(dir, id)
		if err != nil {
			return nil, err
		}
		out = append(out, kp)
	}
	return out, nil
}

func loadOrCreate(dir, id string) (KeyPair, error) {
	kp, err := loadKeyPair(dir, id)
	if err == nil {
		return kp, nil
	}
	if !os.IsNotExist(err) {
		return KeyPair{}, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	if err := os.WriteFile(privatePath(dir, id), priv, 0o600); err != nil {
		return KeyPair{}, err
	}
	if err := os.WriteFile(publicPath(dir, id), pub, 0o644); err != nil {
		return KeyPair{}, err
	}
	return KeyPair{ID: id, Private: priv, Public: pub}, nil
}

func loadKeyPair(dir, id string) (KeyPair, error) {
	priv, err := os.ReadFile(privatePath(dir, id))
	if err != nil {
		return KeyPair{}, err
	}
	if len(priv) != ed25519.PrivateKeySize {
		return KeyPair{}, fmt.Errorf("producer: %s: private key is %d bytes, want %d",
			id, len(priv), ed25519.PrivateKeySize)
	}
	pub, err := os.ReadFile(publicPath(dir, id))
	if err != nil {
		return KeyPair{}, err
	}
	if len(pub) != ed25519.PublicKeySize {
		return KeyPair{}, fmt.Errorf("producer: %s: public key is %d bytes, want %d",
			id, len(pub), ed25519.PublicKeySize)
	}
	return KeyPair{ID: id, Private: ed25519.PrivateKey(priv), Public: ed25519.PublicKey(pub)}, nil
}

func privatePath(dir, id string) string { return filepath.Join(dir, id+privateKeySuffix) }
func publicPath(dir, id string) string  { return filepath.Join(dir, id+publicKeySuffix) }
