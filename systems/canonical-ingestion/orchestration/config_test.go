package orchestration

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigurationKeepsNativeIdentityAndBounds(t *testing.T) {
	c := Defaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mode", "run", "stream", "follow", "managed", "max-published-ahead-records", "cpu-workers", "env-file", "prepare-keys"} {
		s := Defaults()
		s.DHT.Options = map[string]string{key: "other"}
		if s.Validate() == nil {
			t.Fatal("identity/control override accepted", key)
		}
	}
	for _, bad := range []func(*Config){func(c *Config) { c.MaxAhead = nativeEpochSize - 1 }, func(c *Config) { c.Poll = 0 }, func(c *Config) { c.DHT.IOWorkers = 65 }, func(c *Config) { c.BLAS.SQLPoolSize = 1 }} {
		s := Defaults()
		bad(&s)
		if s.Validate() == nil {
			t.Fatal("unbounded configuration accepted")
		}
	}
	if _, err := Mode(10000); err == nil {
		t.Fatal("production accepted test-only checkpoint geometry")
	}
	if mode, err := Mode(10000000); err != nil || mode != "10M" {
		t.Fatal(mode, err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "comparison.yaml")
	if err := os.WriteFile(path, []byte("root: .\nrun_id: existing-run\npoll_interval: 100ms\nmva:\n  cpu_workers: 2\n  options:\n    queue-max-bytes: '67108864'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunID != "existing-run" || loaded.Root != dir || loaded.MVA.IOWorkers != c.MVA.IOWorkers || loaded.Poll != 100*time.Millisecond || loaded.MVA.Options["queue-max-bytes"] != "67108864" {
		t.Fatal(loaded)
	}
	if err = os.WriteFile(path, []byte("unknown: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("unknown configuration ignored")
	}
}
