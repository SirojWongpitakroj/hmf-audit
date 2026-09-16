// Package provision implements the M0 "cert/key generation tooling":
// generating each participant's secp256k1 protocol key (CRYPTO-009: key
// generation at provisioning, local storage, no runtime PKI) and mutual-TLS
// certificate (SYS-002/GAP-48), plus a ready-to-use domain config
// (internal/config) listing every participant in a fixed P order.
//
// This package is deployment tooling; it implements no DELIA algorithm.
package provision

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"delia/internal/pki"
	"delia/sig"
)

// ParticipantSpec is one participant to provision.
type ParticipantSpec struct {
	ID       string
	Endpoint string
	// DNSNames/IPs the participant's TLS certificate should be valid for
	// (typically the host part of Endpoint).
	SANs []string
}

// Result is what Domain produced for one participant.
type Result struct {
	ID         string
	PrivateKey []byte // raw 32-byte secp256k1 scalar (CRYPTO-009: local storage)
	PublicKey  []byte // canonical compressed encoding (GAP-73)
	TLSCertPEM []byte
	TLSKeyPEM  []byte
}

// Domain provisions a full domain: a private test CA, then one secp256k1
// key pair and one mTLS leaf certificate per participant, in the order
// specs is given (that order becomes the P order frozen at Init,
// DATA-013/GAP-08).
func Domain(caCommonName string, specs []ParticipantSpec) (caCertPEM, caKeyPEM []byte, results []Result, err error) {
	caCertPEM, caKeyPEM, err = pki.GenerateCA(caCommonName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("provision: GenerateCA: %w", err)
	}

	for _, spec := range specs {
		kp, err := sig.Gen()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("provision: sig.Gen(%s): %w", spec.ID, err)
		}
		certPEM, keyPEM, err := pki.IssueCert(caCertPEM, caKeyPEM, spec.ID, spec.SANs)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("provision: IssueCert(%s): %w", spec.ID, err)
		}
		results = append(results, Result{
			ID:         spec.ID,
			PrivateKey: kp.Bytes(),
			PublicKey:  kp.PublicKey(),
			TLSCertPEM: certPEM,
			TLSKeyPEM:  keyPEM,
		})
	}
	return caCertPEM, caKeyPEM, results, nil
}

// domainConfigParticipant mirrors config.Participant's JSON shape without
// importing internal/config, so this package stays a leaf (config already
// depends on sig; provision writing config's exact JSON shape here avoids a
// dependency cycle risk and keeps this package's own contract minimal).
type domainConfigParticipant struct {
	ID           string `json:"id"`
	Endpoint     string `json:"endpoint"`
	PublicKeyHex string `json:"public_key"`
}

type domainConfigFile struct {
	Theta        int                       `json:"theta"`
	Participants []domainConfigParticipant `json:"participants"`
}

// WriteToDir writes the CA, one <id>.key/<id>.tls.pem/<id>.tls.key set per
// participant, and a domain.json config (internal/config-compatible) into
// outDir. theta is written into the config as required configuration
// (DATA-007/GAP-55: the caller must supply it; there is no default).
func WriteToDir(outDir string, theta int, specs []ParticipantSpec) error {
	caCertPEM, caKeyPEM, results, err := Domain("DELIA test CA", specs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "ca.pem"), caCertPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "ca.key"), caKeyPEM, 0o600); err != nil {
		return err
	}

	specByID := map[string]ParticipantSpec{}
	for _, s := range specs {
		specByID[s.ID] = s
	}

	cfg := domainConfigFile{Theta: theta}
	for _, r := range results {
		if err := os.WriteFile(filepath.Join(outDir, r.ID+".key"), r.PrivateKey, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, r.ID+".tls.pem"), r.TLSCertPEM, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, r.ID+".tls.key"), r.TLSKeyPEM, 0o600); err != nil {
			return err
		}
		cfg.Participants = append(cfg.Participants, domainConfigParticipant{
			ID:           r.ID,
			Endpoint:     specByID[r.ID].Endpoint,
			PublicKeyHex: hex.EncodeToString(r.PublicKey),
		})
	}
	// cfg.Participants is already in specs order (the P order frozen at
	// Init, DATA-013/GAP-08) — no re-sorting.

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "domain.json"), raw, 0o644)
}
