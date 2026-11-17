package canonicalingestion

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditIdentityAndMemory(t *testing.T) {
	request := AuditRequest{Version: 1, StreamID: "stream", Fingerprint: "index-v1", ExpectedN: 1000000, PrefixSHA256: "0000000000000000000000000000000000000000000000000000000000000000", Entries: []Entry{entry(1), entry(9)}}
	limits := Limits{MaxRecords: 10, MaxBatchBytes: 8192, MaxRecordBytes: 1024}
	if err := request.Validate(limits); err != nil {
		t.Fatal(err)
	}
	digest := request.Digest()
	request.ExpectedN = 10000000
	request.PrefixSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if request.Digest() != digest {
		t.Fatal("fixed workload changed with N")
	}
	request.Entries[1] = entry(10)
	if request.Digest() == digest {
		t.Fatal("changed observation did not change workload digest")
	}
	request.Entries[1] = request.Entries[0]
	if request.Validate(limits) == nil {
		t.Fatal("duplicate audit record accepted")
	}
	before := ReadMemory()
	if before.HeapAllocBytes == 0 {
		t.Fatal(before)
	}
	monitor := StartMemoryMonitor(time.Millisecond)
	peak := monitor.Stop()
	if before.RSSAvailable && peak == 0 {
		t.Fatal("RSS sampler returned zero")
	}
}
func TestDeploymentEnvironment(t *testing.T) {
	t.Setenv("CONTRACT_ENV_REFERENCE", "runner-only")
	t.Setenv("CONTRACT_ENV_EXISTING", "inherited")
	file := filepath.Join(t.TempDir(), "profile.env")
	if err := os.WriteFile(file, []byte("CONTRACT_ENV_ALIAS=${CONTRACT_ENV_REFERENCE}\nCONTRACT_ENV_LITERAL='literal$with#characters'\nCONTRACT_ENV_EXISTING=replaced\n"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("CONTRACT_ENV_ALIAS")
	defer os.Unsetenv("CONTRACT_ENV_LITERAL")
	if err := LoadEnv(file); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CONTRACT_ENV_ALIAS") != "runner-only" || os.Getenv("CONTRACT_ENV_LITERAL") != "literal$with#characters" || os.Getenv("CONTRACT_ENV_EXISTING") != "inherited" {
		t.Fatal("environment reference, password literal or override semantics changed")
	}
	for _, suffix := range []string{"DSN", "HOST", "DATABASE", "USER", "PASSWORD"} {
		t.Setenv("CONTRACT_BASELINES_"+suffix, "")
	}
	t.Setenv("MVA_CP_USER", "old-service")
	t.Setenv("MVA_CP_PASSWORD", "must-not-be-reused")
	if _, err := PostgresDSN("CONTRACT_BASELINES", ""); err == nil {
		t.Fatal("missing new baseline credentials accepted")
	}
}
