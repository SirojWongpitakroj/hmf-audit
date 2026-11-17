package cassandradht

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveDeploymentAndStrictConfiguration(t *testing.T) {
	raw, err := os.ReadFile("deployment.json")
	if err != nil {
		t.Fatal(err)
	}
	var d Deployment
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if err = d.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"10.10.1.66", "10.10.1.194", "10.10.1.205", "10.10.1.58"}
	if len(d.Hosts) != len(want) {
		t.Fatal(d.Hosts)
	}
	for i, h := range want {
		if d.Hosts[i] != h {
			t.Fatal(d.Hosts)
		}
	}
	g := newGate(t)
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "ca.pk"), g.pk, 0600); err != nil {
		t.Fatal(err)
	}
	d.CAFile = "ca.pk"
	raw, _ = json.Marshal(d)
	path := filepath.Join(dir, "deployment.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, cfg, err := LoadDeployment(path)
	if err != nil || cfg.Peers != 4 || cfg.Concurrency != d.Limits.Workers {
		t.Fatal(cfg, err)
	}
	d.LogicalNodes = 16
	if err = d.Validate(); err != nil {
		t.Fatal("configurable power-of-two logical deployment rejected", err)
	}
	d.LogicalNodes = 1024
	if err = d.Validate(); err != nil {
		t.Fatal(err)
	}
	raw1024, _ := json.Marshal(d)
	if err = os.WriteFile(path, raw1024, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, model, err := LoadDeployment(path)
	if err != nil || model.Peers != 1024 || len(loaded.Hosts) != 4 {
		t.Fatal("logical size changed physical hosts", err)
	}
	d.LogicalNodes = 3
	if err = d.Validate(); err == nil {
		t.Fatal("unsupported non-power-of-two XOR topology accepted")
	}
	d.LogicalNodes = 4
	d.Keyspace = "unsafe;statement"
	if err = d.Validate(); err == nil {
		t.Fatal("unsafe keyspace accepted")
	}
	for _, input := range []string{`{"Peers":4,"Endpoints":[]}`, string(raw) + ` {}`} {
		if err = os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err = LoadDeployment(path); err == nil {
			t.Fatal("legacy/multiple deployment objects accepted")
		}
	}
}
