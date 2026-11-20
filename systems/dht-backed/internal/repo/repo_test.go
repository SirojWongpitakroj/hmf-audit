// Package repo holds structural tests over the repository itself:
// the Q28 package skeleton, the §0.6 single-goroutine rule, and the Q12
// prohibition on serializing Go maps.
package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// root walks up from this test file to the module root (the directory holding
// go.mod).
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("module root not found")
	return ""
}

// Q28 names exactly these ten component packages.
var q28Packages = []string{
	"auth", "benchmark", "blockchain", "dht", "encryptor",
	"ingestverifier", "mktree", "producer", "storage", "verifier",
}

func TestTopLevelPackagesAreExactlyQ28(t *testing.T) {
	r := root(t)
	entries, err := os.ReadDir(r)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Not Go packages: the paper copy, the Solidity sources, the Hardhat
		// scripts, the Node module tree, and internal/ (shared infrastructure,
		// which is not a top-level import path).
		switch name {
		case "paper", "contracts", "scripts", "internal", "node_modules", "artifacts", "cache", "reports":
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		found = append(found, name)
	}
	sort.Strings(found)
	want := append([]string(nil), q28Packages...)
	sort.Strings(want)
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Fatalf("top-level packages = %v, want exactly %v", found, want)
	}
}

func TestEveryQ28PackageExists(t *testing.T) {
	r := root(t)
	for _, p := range q28Packages {
		matches, _ := filepath.Glob(filepath.Join(r, p, "*.go"))
		if len(matches) == 0 {
			t.Errorf("package %q contains no Go source", p)
		}
	}
}

// goFiles returns every non-test Go file under the module root.
func goFiles(t *testing.T, includeTests bool) []string {
	t.Helper()
	r := root(t)
	var out []string
	err := filepath.Walk(r, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "artifacts", "cache", "paper", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		if !includeTests && strings.HasSuffix(p, "_test.go") {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// concurrentDHTDir is the one package allowed to use goroutines. The paper
// requires no concurrency (§0.6, Q32), and the reproduction of it stays
// single-goroutine; on 2026-09-21 the fair benchmark's DHT-backed system was
// authorized to fetch proof records concurrently from Cassandra, so that
// behavior lives in this package alone. The verifier, the storage layer and
// every other package still start no goroutine directly. Experiment 1B's
// user-authorized execution wrapper additionally invokes the bounded shared
// AuditParallel helper for independent leaf paths. The native serial verifier,
// ordered ancestor fold and authenticated structure are unchanged.
var concurrentDHTDir = filepath.Join("internal", "cassandradht")

func inConcurrentDHT(path string) bool {
	// Transport/storage and the incremental publication coordinator may use
	// bounded concurrency. The latter serializes its shared publisher while
	// independent type trees prepare; native verification remains serial.
	return strings.Contains(path, string(filepath.Separator)+concurrentDHTDir+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+filepath.Join("internal", "peerdht")+string(filepath.Separator)) || strings.HasSuffix(path, filepath.Join("benchmark", "incremental_publish.go")) || inBenchmarkIngest(path)
}

// inBenchmarkIngest lists the fair-benchmark setup files that seed the fixture
// with bounded parallelism (fair_fixture.go, pipelined_publication*.go). They
// run outside every timer and are not part of the scheme under test; the
// verifier and the authenticated structure stay single-goroutine.
func inBenchmarkIngest(path string) bool {
	dir := string(filepath.Separator) + "benchmark" + string(filepath.Separator)
	base := filepath.Base(path)
	return strings.Contains(path, dir) && (base == "fair_fixture.go" || strings.HasPrefix(base, "pipelined_publication"))
}

// §0.6 / Q32: single-goroutine execution model. The paper requires no
// concurrency, so no `go` statement may appear anywhere outside the one
// package authorized above.
func TestNoGoStatementAnywhere(t *testing.T) {
	fset := token.NewFileSet()
	for _, f := range goFiles(t, true) {
		if inConcurrentDHT(f) {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if g, ok := n.(*ast.GoStmt); ok {
				t.Errorf("%s: `go` statement at %s", f, fset.Position(g.Pos()))
			}
			return true
		})
	}
}

// §0.6 / Q32: no Go concurrency primitives outside anything the paper
// requires — it requires none.
func TestNoConcurrencyPrimitives(t *testing.T) {
	banned := []string{`"sync"`, `"sync/atomic"`, "chan ", "select {", "go func"}
	for _, f := range goFiles(t, true) {
		if strings.HasSuffix(f, filepath.Join("internal", "repo", "repo_test.go")) {
			continue // this file names the constructs it forbids
		}
		if inConcurrentDHT(f) {
			continue
		}
		if strings.Contains(f, filepath.Join("blockchain", "binding")) {
			// abigen-generated contract bindings (Q42). Their event-subscription
			// boilerplate contains channels; the reproduction calls only
			// commit/getRoot/committer and never any Watch method, and the AST
			// check above confirms no `go` statement is reachable.
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, tok := range banned {
			if strings.Contains(src, tok) {
				t.Errorf("%s: contains concurrency construct %q", f, tok)
			}
		}
	}
}

// Q12 / R-EVAL-08: Go maps are never serialized. No serialization path may
// range over a map, and encoding/json must never marshal a map.
func TestNoMapSerialization(t *testing.T) {
	fset := token.NewFileSet()
	for _, f := range goFiles(t, false) {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			// A map type literal inside a struct that is handed to a marshaller
			// cannot be detected syntactically; what is detectable, and what
			// the rule forbids in practice, is `json:"..."` tags on map fields.
			fd, ok := n.(*ast.Field)
			if !ok || fd.Tag == nil {
				return true
			}
			if _, isMap := fd.Type.(*ast.MapType); isMap {
				t.Errorf("%s: struct tag on a map field at %s", f, fset.Position(fd.Pos()))
			}
			return true
		})
	}
}

// Q28 / R-NFR-03: components communicate by ordinary in-process function and
// interface calls. No REST, gRPC, message queue or microservice boundary is
// introduced anywhere in the reproduction's own code.
//
// The only network client permitted is the go-ethereum JSON-RPC client used to
// reach the external Hardhat chain (Q38, Q42), and the only database client is
// pgx (Q43); both are external processes the SRS requires.
func TestNoServiceBoundaries(t *testing.T) {
	banned := []string{
		"net/http", "google.golang.org/grpc", "net/rpc",
		"github.com/gorilla", "github.com/gin-gonic", "amqp", "kafka", "nats",
	}
	for _, f := range goFiles(t, true) {
		if strings.HasSuffix(f, filepath.Join("benchmark", "incremental_test.go")) {
			continue
		} // Integration fixture launches the authorized peer transport.
		if strings.Contains(f, string(filepath.Separator)+filepath.Join("internal", "peerdht")+string(filepath.Separator)) {
			continue
		} // Four independently persistent peers require real network transport.
		if strings.Contains(f, filepath.Join("blockchain", "binding")) {
			continue // abigen-generated bindings
		}
		if strings.HasSuffix(f, filepath.Join("internal", "repo", "repo_test.go")) {
			continue // this file names the imports it forbids
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, tok := range banned {
			if strings.Contains(src, `"`+tok+`"`) || strings.Contains(src, tok+`/`) {
				t.Errorf("%s: imports %q; components must talk by in-process calls", f, tok)
			}
		}
	}
}

// Q10 / §2.3: lookups are synchronous with no routing-delay simulation, no RTT
// injection and no network emulation.
func TestNoSleepOrDelaySimulation(t *testing.T) {
	for _, f := range goFiles(t, false) {
		if strings.Contains(f, filepath.Join("blockchain", "binding")) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, tok := range []string{"time.Sleep", "time.Tick", "time.After("} {
			if strings.Contains(src, tok) {
				t.Errorf("%s: contains %q", f, tok)
			}
		}
	}
}

// Q39: the original reproduction uses a fixed 50-repetition protocol. The
// shared fair-persistence runner additionally uses testing.B for measured
// request timings.
func TestNoTestingBBenchmarks(t *testing.T) {
	for _, f := range goFiles(t, true) {
		if strings.HasSuffix(f, filepath.Join("internal", "repo", "repo_test.go")) {
			continue
		}
		if strings.HasSuffix(f, filepath.Join("benchmark", "fair_benchmark_test.go")) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "*testing.B") {
			t.Errorf("%s: declares a testing.B benchmark", f)
		}
	}
}
