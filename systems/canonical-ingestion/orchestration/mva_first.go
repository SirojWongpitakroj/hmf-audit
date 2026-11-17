package orchestration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	contract "canonicalingestion"
	"gopkg.in/yaml.v3"
)

// Load the selected native environment before the common baseline profile.
// Otherwise the latter's legacy MVA settings would mask the v3 environment in
// the child process. Explicitly inherited shell exports retain precedence.
func loadMVAFirstEnvironment(c Config) error {
	path := c.MVA.Options["config"]
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.Root, "MVA-PHV", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var profile struct {
		EnvFile   string `yaml:"env_file"`
		Admission struct {
			Mode string `yaml:"mode"`
		} `yaml:"admission"`
	}
	if err = yaml.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&profile); err != nil {
		return err
	}
	if profile.Admission.Mode != "logical-aging-v3" || profile.EnvFile == "" {
		return fmt.Errorf("v3 orchestration requires logical-aging-v3 with an explicit env_file")
	}
	if !filepath.IsAbs(profile.EnvFile) {
		profile.EnvFile = filepath.Join(filepath.Dir(path), profile.EnvFile)
	}
	if err = contract.LoadEnv(profile.EnvFile); err != nil {
		return fmt.Errorf("load MVA v3 environment: %w", err)
	}
	// Native MVA uses the six separate settings, not a DSN override. Prevent
	// supervisor snapshots from silently opening a different legacy journal.
	for _, suffix := range []string{"HOST", "PORT", "DATABASE", "USER", "PASSWORD", "SSLMODE"} {
		if os.Getenv("MVA_CHECKPOINT_"+suffix) == "" {
			return fmt.Errorf("MVA v3 environment requires MVA_CHECKPOINT_%s", suffix)
		}
	}
	return nil
}

func baselineAlreadyComplete(name string, p Progress) (bool, error) {
	var cursor Cursor
	switch name {
	case "delia":
		cursor = p.DELIA
	case "blas":
		cursor = p.BLAS
	case "dht":
		cursor = p.DHT
	default:
		return false, fmt.Errorf("unknown baseline %q", name)
	}
	proofReady := name != "dht" || cursor.ProofComplete >= p.Target
	if cursor.Committed >= p.Target && cursor.Completed && proofReady {
		return true, nil
	}
	if cursor.Committed > p.Target {
		return false, fmt.Errorf("%s is past target %d without a completed stage/proof prefix; finish its active milestone first", name, p.Target)
	}
	return false, nil
}
