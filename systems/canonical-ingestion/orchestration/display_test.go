package orchestration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contract "canonicalingestion"
)

func TestConciseProgressKeepsActivitySeparateFromDurableState(t *testing.T) {
	var out bytes.Buffer
	emit, err := progressEncoder(&out, "text")
	if err != nil {
		t.Fatal(err)
	}
	p := Progress{Time: time.Unix(100, 0), Target: 1000000, Fingerprint: "hidden-fingerprint", PrefixHash: "hidden-prefix", Children: map[string]Telemetry{}}
	for _, name := range []string{"mva", "delia", "blas", "dht"} {
		p.Children[name] = Telemetry{Memory: contract.MemorySnapshot{RSSAvailable: true, RSSBytes: 1 << 20}}
	}
	m := p.Children["mva"]
	m.StagedRecords = 26605
	m.RollingAcceptedRecordsPerSecond = 501
	p.Children["mva"] = m
	if err = emit(p); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"RUNNING", "Target 1,000,000", "26,605 processed this session", "~501 rec/s", "Finalized 0", "Published 0", "DHT accepted 0 (0 rec/s), proofs 0", "Workers RSS 4 MiB"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q: %s", want, s)
		}
	}
	for _, hidden := range []string{p.Fingerprint, p.PrefixHash, "PageCopyBytes", "StorageAccess", "CPUWorkers"} {
		if strings.Contains(s, hidden) {
			t.Fatalf("verbose diagnostic leaked: %s", hidden)
		}
	}
	// No changes to cursors or completion can be caused by rendering.
	if p.MVAFinalized != 0 || p.Published != 0 || p.Complete() {
		t.Fatal("activity became durable progress")
	}
}

func TestConciseProgressShowsEachBaselineIngestionRate(t *testing.T) {
	var out bytes.Buffer
	emit, _ := progressEncoder(&out, "text")
	p := Progress{
		DELIA:       Cursor{Committed: 100000, Rate: 0},
		BLAS:        Cursor{Committed: 15000, Rate: 200.4},
		DHT:         Cursor{Committed: 90000, Rate: 350.6},
		DHTAccepted: 90000, DHTProofComplete: 80000,
	}
	if err := emit(p); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DELIA 100,000 (0 rec/s)", "BLAS 15,000 (200 rec/s)", "DHT accepted 90,000 (351 rec/s), proofs 80,000"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}

func TestConciseFinalStatusRequiresProofsAndNativeMilestones(t *testing.T) {
	p := Progress{Target: 10, MVAFinalized: 10, Published: 10, Final: true, DELIA: Cursor{Committed: 10, Completed: true}, BLAS: Cursor{Committed: 10, Completed: true}, DHT: Cursor{Committed: 10, ProofComplete: 9, Completed: true}, DHTAccepted: 10, DHTProofComplete: 9}
	var out bytes.Buffer
	emit, _ := progressEncoder(&out, "text")
	emit(p)
	if !strings.Contains(out.String(), "STOPPED") || strings.Contains(out.String(), "COMPLETE") {
		t.Fatal("accepted-only DHT falsely completed")
	}
	p.DHT.ProofComplete = 10
	p.DHTProofComplete = 10
	out.Reset()
	emit(p)
	if !strings.Contains(out.String(), "COMPLETE") {
		t.Fatal("complete native state not reported")
	}
	p.DHT.Completed = false
	out.Reset()
	emit(p)
	if strings.Contains(out.String(), "COMPLETE") {
		t.Fatal("missing native milestone ignored")
	}
}

func TestProgressJSONRemainsAvailableAndErrorsPropagate(t *testing.T) {
	p := Progress{Target: 100, Published: 20, Fingerprint: "keep-json-fields"}
	var out bytes.Buffer
	emit, _ := progressEncoder(&out, "json")
	if err := emit(p); err != nil {
		t.Fatal(err)
	}
	var got Progress
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Target != p.Target || got.Published != p.Published || got.Fingerprint != p.Fingerprint {
		t.Fatal("JSON contract lost", err)
	}
	if _, err := progressEncoder(&out, "unknown"); err == nil {
		t.Fatal("unknown format accepted")
	}
	for _, format := range []string{"text", "json"} {
		emit, _ := progressEncoder(failedProgressWriter{}, format)
		if err := emit(p); err == nil {
			t.Fatal("output failure ignored", format)
		}
	}
}

type failedProgressWriter struct{}

func (failedProgressWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestConciseTelemetryDoesNotReusePreviousSessionActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mva.log")
	old := []byte("{\"CPUPercentOneCore\":50,\"records_staged_this_process\":99999}\n{\"orchestrator_session_start\":true}\n")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	var p Progress
	children := []*child{{name: "mva", path: path}}
	attachTelemetry(&p, children)
	if _, ok := p.Children["mva"]; ok {
		t.Fatal("previous session activity leaked")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"CPUPercentOneCore\":50,\"records_staged_this_process\":123,\"RollingAcceptedRecordsPerSecond\":42}\n")
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	attachTelemetry(&p, children)
	if p.Children["mva"].StagedRecords != 123 || p.Children["mva"].RollingAcceptedRecordsPerSecond != 42 {
		t.Fatal("native activity fields were not decoded")
	}
}
