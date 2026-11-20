package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"

	"dhtbacked/internal/hashing"
)

type cloudDHTTrust struct {
	Root       hashing.Digest
	PrivateKey []byte
}

// Keep the fixture's auditor CA and reference root together so a dashboard
// process never has to trust a root read from its proof store.
func saveCloudDemoDHTTrust(dataset, workDir string, root hashing.Digest, count uint64) error {
	if count != 10_000_000 || os.Getenv("FAIR_CLOUD") != "1" {
		return nil
	}
	dir := os.Getenv("FAIR_DASHBOARD_TRUST_DIR")
	if dir == "" {
		systems := filepath.Dir(filepath.Dir(dataset))
		dir = filepath.Join(systems, "demo-scripts-v2", "site", ".dashboard", "trust")
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil
		}
	}
	key, err := os.ReadFile(filepath.Join(workDir, "ca.sk"))
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "dht-trust-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	writeErr := json.NewEncoder(tmp).Encode(cloudDHTTrust{Root: root, PrivateKey: key})
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "dht-backed.json"))
}
