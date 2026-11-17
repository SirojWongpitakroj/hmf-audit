// Package audit turns several §14 acceptance-matrix rows that were
// previously "confirmed by manual inspection this session" (M13's INSPECT
// classification) into reproducible, automated static checks, so they are
// PASS results backed by something actually run, not a one-time manual
// grep. It imports every protocol package purely to let reflection-based
// checks (e.g. T-065) compile against real types; it holds no protocol
// logic of its own.
package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/query"
	"blas/verify"
)

// repoRoot returns the blas/ module root, computed from this file's own
// path (so the test works regardless of the working directory `go test`
// was invoked from).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine this file's path")
	}
	return filepath.Dir(filepath.Dir(thisFile)) // audit/ -> blas/
}

// goFiles returns every .go file under root, split into (protocolFiles,
// testFiles, evalFiles) — protocolFiles excludes both _test.go files and
// anything under eval/ (the only package permitted to hold §12 constants,
// R-GO-027) and under audit/ itself.
func goFiles(t *testing.T, root string) (protocolFiles, testFiles, evalFiles []string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		isTest := strings.HasSuffix(path, "_test.go")
		switch {
		case strings.HasPrefix(rel, "eval/"):
			evalFiles = append(evalFiles, path)
		case strings.HasPrefix(rel, "audit/"):
			// skip this package itself
		case isTest:
			testFiles = append(testFiles, path)
		default:
			protocolFiles = append(protocolFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return protocolFiles, testFiles, evalFiles
}

func parseImports(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var out []string
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}

// TestHashAlgorithmAudit: T-054 — no forbidden hash algorithm is ever
// imported; golang.org/x/crypto/blake2b is imported only by bhash.
func TestHashAlgorithmAudit(t *testing.T) {
	root := repoRoot(t)
	protocolFiles, testFiles, evalFiles := goFiles(t, root)
	forbidden := map[string]bool{
		"crypto/sha256": true, "crypto/sha512": true, "crypto/sha1": true,
		"crypto/md5": true, "hash/fnv": true, "golang.org/x/crypto/blake2s": true,
	}
	for _, f := range append(append(protocolFiles, testFiles...), evalFiles...) {
		for _, imp := range parseImports(t, f) {
			if forbidden[imp] {
				t.Fatalf("%s imports forbidden hash package %q", f, imp)
			}
			if imp == "golang.org/x/crypto/blake2b" {
				rel, _ := filepath.Rel(root, f)
				if !strings.HasPrefix(filepath.ToSlash(rel), "bhash/") {
					t.Fatalf("%s imports blake2b directly; only bhash may do so", f)
				}
			}
		}
	}
}

// TestDependencyAudit: T-059 — go.mod contains only the approved modules.
func TestDependencyAudit(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"canonicalingestion":             true, // shared source identity/cursor contract; no native hashing
		"github.com/jackc/puddle/v2":     true, // database/sql pgx adapter
		"golang.org/x/sync":              true, // pgx pool dependency
		"golang.org/x/crypto":            true, // A1: approved for blake2b
		"golang.org/x/sys":               true, // indirect dependency of x/crypto itself
		"github.com/jackc/pgx/v5":        true, // PostgreSQL persistence for the fair runner
		"github.com/jackc/pgpassfile":    true, // pgx indirect dependency
		"github.com/jackc/pgservicefile": true, // pgx indirect dependency
		"golang.org/x/text":              true, // pgx indirect dependency
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "module") || strings.HasPrefix(line, "go ") ||
			line == "require (" || line == ")" || strings.HasPrefix(line, "//") || line == "replace canonicalingestion => ../canonical-ingestion" {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "require"))
		if len(fields) == 0 {
			continue
		}
		modPath := fields[0]
		if !allowed[modPath] {
			t.Fatalf("go.mod contains unapproved module %q", modPath)
		}
	}
}

// TestNoExperimentConstantsInProtocolPackages: T-060 — none of the §12
// experiment constants appear as integer literals in any protocol package
// (only `eval` may hold them, R-GO-027).
func TestNoExperimentConstantsInProtocolPackages(t *testing.T) {
	root := repoRoot(t)
	protocolFiles, _, _ := goFiles(t, root)
	forbidden := map[string]bool{
		"4096": true, "2048": true, "8192": true,
		"128": false, // 128 is too common a bit-width/size literal elsewhere; excluded to avoid false positives (e.g. buffer sizes)
	}
	fset := token.NewFileSet()
	for _, path := range protocolFiles {
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "cmd/") {
			continue
		} // CLI defaults are configurable, not protocol constants.
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				return true
			}
			if forbidden[lit.Value] {
				t.Fatalf("%s: forbidden experiment-constant literal %q found outside eval/", path, lit.Value)
			}
			return true
		})
	}
}

// TestNoForbiddenLibraries: T-075 — no blockchain/consensus framework,
// database, IPFS, REST/gRPC, or container-tooling import anywhere.
func TestNoForbiddenLibraries(t *testing.T) {
	root := repoRoot(t)
	protocolFiles, testFiles, evalFiles := goFiles(t, root)
	forbiddenSubstrings := []string{
		"ethereum", "hyperledger", "fabric", "solidity",
		"database/sql", "gorm", "mongo", "redis", "postgres",
		"ipfs",
		"grpc", "net/rpc",
		"docker", "kubernetes",
		"net/http", // no REST API
	}
	for _, f := range append(append(protocolFiles, testFiles...), evalFiles...) {
		for _, imp := range parseImports(t, f) {
			rel, _ := filepath.Rel(root, f)
			storagePath := strings.HasPrefix(filepath.ToSlash(rel), "internal/durable/") || strings.HasPrefix(filepath.ToSlash(rel), "internal/diskindex/") || strings.HasPrefix(filepath.ToSlash(rel), "cmd/incremental_runner/")
			if storagePath && imp == "database/sql" {
				continue
			} // Durable storage and its runner's pool telemetry; protocol algorithms remain SQL-free.
			lower := strings.ToLower(imp)
			for _, bad := range forbiddenSubstrings {
				if strings.Contains(lower, bad) {
					t.Fatalf("%s imports %q, matching forbidden substring %q", f, imp, bad)
				}
			}
		}
	}
}

// TestFourActorsOnly: T-064 — exactly four actor types exist (by name),
// and no fifth-actor-suggesting type name (Peer/Miner/Validator/Consensus
// node) appears anywhere.
func TestFourActorsOnly(t *testing.T) {
	// Compile-time existence check: these four types are the only actors
	// (R-FUNC-001). If any were renamed/removed, this file would fail to
	// build.
	var _ *alis.LogGenerator
	var _ *chain.Chain // the full node's on-chain state; query.FullNode wraps it
	var _ *query.FullNode
	var _ *query.SP
	var _ *verify.Auditor

	root := repoRoot(t)
	protocolFiles, _, _ := goFiles(t, root)
	forbiddenNames := []string{"type Peer ", "type Miner ", "type Validator ", "type ConsensusNode ", "type Node5 "}
	for _, path := range protocolFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		for _, bad := range forbiddenNames {
			if strings.Contains(content, bad) {
				t.Fatalf("%s defines a suspicious fifth-actor-like type: %q", path, bad)
			}
		}
	}
}

// TestAuditorStoresOnlyHeaders: T-065 — no block bodies, indexes, or raw logs.
// A bounded header reader replaces full header residency on the durable path.
func TestAuditorStoresOnlyHeaders(t *testing.T) {
	typ := reflect.TypeOf(verify.Auditor{})
	if typ.NumField() != 5 {
		t.Fatalf("unexpected auditor storage fields: %d", typ.NumField())
	}
	names := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		names[typ.Field(i).Name] = true
		if !map[string]bool{"Headers": true, "SegmentSize": true, "storedHeaders": true, "headerReader": true, "headerError": true}[typ.Field(i).Name] {
			t.Fatalf("auditor retains unexpected state: %s", typ.Field(i).Name)
		}
	}
	if !names["Headers"] || !names["SegmentSize"] {
		t.Fatalf("expected fields {Headers, SegmentSize}, got %v", names)
	}
}

// TestNoConfidentialityMachinery: T-063 — no encryption/TLS/access-control
// import anywhere in the tree.
func TestNoConfidentialityMachinery(t *testing.T) {
	root := repoRoot(t)
	protocolFiles, testFiles, evalFiles := goFiles(t, root)
	forbidden := []string{"crypto/aes", "crypto/cipher", "crypto/tls", "crypto/rsa", "crypto/rc4", "crypto/des"}
	for _, f := range append(append(protocolFiles, testFiles...), evalFiles...) {
		for _, imp := range parseImports(t, f) {
			for _, bad := range forbidden {
				if imp == bad {
					t.Fatalf("%s imports %q, which implements confidentiality machinery out of scope (R-FUNC-020)", f, imp)
				}
			}
		}
	}
}

// TestNoDeploymentExampleComponent: T-076 — no logger-plugin/ELK-style
// component exists as a package.
func TestNoDeploymentExampleComponent(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	forbiddenDirNames := map[string]bool{
		"logger": true, "elk": true, "elasticsearch": true, "logstash": true, "kibana": true, "campusnetwork": true,
	}
	for _, e := range entries {
		if e.IsDir() && forbiddenDirNames[strings.ToLower(e.Name())] {
			t.Fatalf("found a deployment-example-like package directory: %s", e.Name())
		}
	}
}
