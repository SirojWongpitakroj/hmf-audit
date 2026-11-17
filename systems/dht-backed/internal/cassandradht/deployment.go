package cassandradht

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	contract "canonicalingestion"
	"dhtbacked/auth"
)

// Hosts are Cassandra contact points, never physical DHT peer endpoints.
type Deployment struct {
	LogicalNodes                            int
	Overlay, CAFile, Keyspace, LocalDC      string
	Hosts                                   []string
	Port, ReplicationFactor, TimeoutSeconds int
	Limits                                  Limits
}

var validKeyspace = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func (d Deployment) Validate() error {
	if _, err := contract.LogicalDHTHops(d.LogicalNodes); err != nil {
		return err
	}
	if d.Overlay == "" || len(d.Hosts) == 0 || d.Port < 1 || d.Port > 65535 || !validKeyspace.MatchString(d.Keyspace) || d.ReplicationFactor < 1 || d.TimeoutSeconds < 1 || d.TimeoutSeconds > 300 {
		return fmt.Errorf("cassandradht: bounded logical identities and explicit Cassandra deployment required")
	}
	seen := map[string]bool{}
	for _, h := range d.Hosts {
		if h == "" || seen[h] {
			return fmt.Errorf("cassandradht: empty/duplicate Cassandra contact point")
		}
		seen[h] = true
	}
	return d.Limits.Validate()
}
func (c Config) ValidateActive() error {
	if _, err := contract.LogicalDHTHops(c.Peers); err != nil {
		return err
	}
	if c.Overlay == "" || len(c.CA) != 32 || c.Limits == nil || c.Concurrency != c.Limits.Workers {
		return fmt.Errorf("cassandradht: active path requires bounded logical identities, authority and bounded configuration")
	}
	return c.Limits.Validate()
}
func LoadDeployment(path string) (Deployment, Config, error) {
	var d Deployment
	b, err := os.ReadFile(path)
	if err != nil {
		return d, Config{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&d); err != nil {
		return d, Config{}, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return d, Config{}, fmt.Errorf("one deployment JSON object required")
	}
	if err = d.Validate(); err != nil {
		return d, Config{}, err
	}
	file := d.CAFile
	if !filepath.IsAbs(file) {
		file = filepath.Join(filepath.Dir(path), file)
	}
	ca, err := auth.LoadCAPublicKey(file)
	if err != nil {
		return d, Config{}, err
	}
	c := Config{Peers: d.LogicalNodes, Concurrency: d.Limits.Workers, Overlay: d.Overlay, CA: ca, Limits: &d.Limits}
	return d, c, c.ValidateActive()
}
