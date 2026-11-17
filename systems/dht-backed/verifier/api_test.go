package verifier_test

// GAP-4 decision (user, 2026-09-11): keep the named rejection reasons, but keep
// them internal and expose only accept/reject as the verifier's result.
// These tests pin that decision so the public surface cannot drift back into
// an error taxonomy.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"dhtbacked/dht"
	"dhtbacked/verifier"
)

// The package's exported errors are exactly the verdict (ErrRejected), the Q52
// refusal, and the invalid-request error. Parsed from the non-test source, so
// the test-only aliases in export_test.go do not count.
func TestPublicErrorSurfaceIsAcceptRejectOnly(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var exported []string
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, decl := range f.Decls {
				g, ok := decl.(*ast.GenDecl)
				if !ok || g.Tok != token.VAR {
					continue
				}
				for _, spec := range g.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						if name.IsExported() && strings.HasPrefix(name.Name, "Err") {
							exported = append(exported, name.Name)
						}
					}
				}
			}
		}
	}
	sort.Strings(exported)
	want := []string{"ErrEpochNotAnchored", "ErrMixedEpochs", "ErrRejected"}
	if strings.Join(exported, ",") != strings.Join(want, ",") {
		t.Fatalf("exported errors = %v, want exactly %v; rejection reasons must stay internal", exported, want)
	}
}

// Every internal rejection reason wraps ErrRejected, so a caller that checks
// errors.Is(err, ErrRejected) sees every rejection — and nothing finer.
func TestEveryRejectionReasonWrapsErrRejected(t *testing.T) {
	for name, e := range map[string]error{
		"record not found":  verifier.ErrRecordNotFound,
		"proof unavailable": verifier.ErrProofUnavailable,
		"proof malformed":   verifier.ErrProofMalformed,
		"path mismatch":     verifier.ErrPathMismatch,
		"anchor mismatch":   verifier.ErrAnchorMismatch,
		"not covered":       verifier.ErrNotCovered,
	} {
		if !errors.Is(e, verifier.ErrRejected) {
			t.Errorf("%s does not wrap ErrRejected", name)
		}
	}
	// The refusal and the invalid-request error are not verdicts.
	for name, e := range map[string]error{
		"open-epoch refusal": verifier.ErrEpochNotAnchored,
		"mixed epochs":       verifier.ErrMixedEpochs,
	} {
		if errors.Is(e, verifier.ErrRejected) {
			t.Errorf("%s wraps ErrRejected, but it is not a verification verdict", name)
		}
	}
}

// End to end: a corrupted proof yields Accepted == false and an error that is
// ErrRejected. The specific reason is still identifiable internally.
func TestTamperSurfacesOnlyAsRejected(t *testing.T) {
	f := build(t, 200, defaultConfig(), true)
	ctx := context.Background()
	id := f.records[0].RecordID
	rec, err := f.store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	levels, _ := geometry(defaultConfig())
	key := dht.KeyP(dht.TreeID(rec.Epoch, rec.LogType), levels, rec.LeafIndex^1)
	original, err := f.sys.DHT.GetRaw(key)
	if err != nil {
		t.Fatal(err)
	}
	f.sys.DHT.PutRaw(key, []byte{0xFF, 0xFF}) // adversary corrupts a sibling record
	defer f.sys.DHT.PutRaw(key, original)

	res, err := f.sys.Verifier.VerifyInclusion(ctx, &f.token, id)
	if res.Accepted {
		t.Fatal("a corrupted proof was accepted")
	}
	if !errors.Is(err, verifier.ErrRejected) {
		t.Fatalf("err = %v, want it to be ErrRejected", err)
	}
	if !errors.Is(err, verifier.ErrProofMalformed) {
		t.Fatalf("internally the reason should still be identifiable as malformed proof: %v", err)
	}
}
