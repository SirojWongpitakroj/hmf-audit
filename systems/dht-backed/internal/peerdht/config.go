package peerdht

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"dhtbacked/auth"
)

// Deployment is infrastructure configuration, independent of all experiments.
// Each peer uses its own environment/DSN; no database credentials are serialized.
type Deployment struct {
	Node                    int
	Listen, Overlay, CAFile string
	Peers                   []string
	Limits                  Limits
	TimeoutSeconds          int
}

func LoadDeployment(path string) (Deployment, Config, error) {
	var d Deployment
	b, err := os.ReadFile(path)
	if err != nil {
		return d, Config{}, err
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, Config{}, err
	}
	caFile := d.CAFile
	if !filepath.IsAbs(caFile) {
		caFile = filepath.Join(filepath.Dir(path), caFile)
	}
	ca, err := auth.LoadCAPublicKey(caFile)
	if err != nil {
		return d, Config{}, err
	}
	secret := os.Getenv("DHT_PEER_WRITE_SECRET")
	if secret == "" {
		return d, Config{}, fmt.Errorf("DHT_PEER_WRITE_SECRET required")
	}
	c := Config{Node: d.Node, Overlay: d.Overlay, Peers: d.Peers, CA: ca, WriteSecret: secret, Limits: d.Limits, Timeout: time.Duration(d.TimeoutSeconds) * time.Second}
	return d, c, c.Validate()
}
