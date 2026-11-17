package verifier_test

// Independent verification of batch cost (M8: R-VER-07, R-VER-08).
//
// Result.HashCount, Result.Ancestors and Result.AncestorPathRecomputations are
// counters the verifier maintains itself. A test that only checks
// "HashCount <= bound" proves as much as those counters' honesty: an
// undercounting counter would pass it trivially.
//
// So these tests compute g — the number of distinct level-s ancestors a batch
// spans — independently, from each record's (log_type, leaf_index) read back
// out of storage, and then require EXACT equality:
//
//   Ancestors                  == g
//   AncestorPathRecomputations == g                  (R-VER-07: g times, not n)
//   HashCount                  == n(L - s) + g*s     (Theorem G.1, with equality)
//
// The verifier hashes exactly L - s times per leaf (leaf -> ancestor) and s
// times per distinct ancestor (ancestor -> root), so equality is what the
// algorithm must produce. Equality cannot be met by a counter that drops
// hashes, and Theorem G.1's "<=" follows from it a fortiori.

import (
	"context"
	"testing"

	"dhtbacked/mktree"
)

// independentG counts distinct (log_type, level-s ancestor) pairs for a batch,
// deriving each record's ancestor from its stored leaf_index without asking
// the verifier.
func independentG(t *testing.T, f *fixture, ids []string, levels, stride uint64) int {
	t.Helper()
	type key struct {
		logType  string
		ancestor uint64
	}
	seen := map[key]bool{}
	for _, id := range ids {
		rec, err := f.store.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		seen[key{rec.LogType, rec.LeafIndex >> (levels - stride)}] = true
	}
	return len(seen)
}

func idsOfType(t *testing.T, f *fixture, logType string) []string {
	t.Helper()
	var out []string
	for _, r := range f.records {
		rec, err := f.store.Get(context.Background(), r.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		if rec.LogType == logType {
			out = append(out, r.RecordID)
		}
	}
	return out
}

// R-VER-07 / R-VER-08 across batches of very different locality: single
// record, subtree-local, scattered, one whole log type, and a mixed-type batch.
func TestIndependentBatchCostMatchesTheoremExactly(t *testing.T) {
	for _, stride := range []uint64{2, 4, 8} {
		cfg := defaultConfig()
		cfg.Stride = stride
		f := build(t, 1000, cfg, true)
		levels, s := geometry(cfg)
		access := idsOfType(t, f, "access")
		if len(access) < 64 {
			t.Fatalf("only %d access records", len(access))
		}

		scattered := []string{}
		for i := 0; i < len(access); i += len(access) / 16 {
			scattered = append(scattered, access[i])
		}
		mixed := []string{}
		for _, r := range f.records[:40] {
			mixed = append(mixed, r.RecordID)
		}

		batches := map[string][]string{
			"single":      access[:1],
			"local-32":    access[:32],
			"scattered":   scattered,
			"whole-type":  access,
			"mixed-types": mixed,
		}
		for name, ids := range batches {
			res, err := f.sys.Verifier.VerifyBatch(context.Background(), &f.token, ids)
			if err != nil || !res.Accepted {
				t.Fatalf("s=%d %s: rejected an honest batch: %v", stride, name, err)
			}
			n := uint64(len(ids))
			g := independentG(t, f, ids, levels, s)

			if res.Ancestors != g {
				t.Errorf("s=%d %s: verifier reports g=%d, independent count is %d", stride, name, res.Ancestors, g)
			}
			if res.AncestorPathRecomputations != g {
				t.Errorf("s=%d %s: ancestor-to-root path walked %d times for g=%d ancestors (n=%d); R-VER-07 requires g, not n",
					stride, name, res.AncestorPathRecomputations, g, n)
			}
			want := n*(levels-s) + uint64(g)*s
			if res.HashCount != want {
				t.Errorf("s=%d %s: HashCount=%d, want exactly n(L-s)+gs = %d*%d + %d*%d = %d",
					stride, name, res.HashCount, n, levels-s, g, s, want)
			}
		}
	}
}

// R-VER-07, sharpest form: a batch of many leaves that all sit under ONE
// level-s ancestor walks the ancestor-to-root path exactly once.
//
// With s = 2 and L = 10 each ancestor covers 2^(10-2) = 256 leaf positions.
// A 1,000-record uniform epoch gives each of the five types roughly 200
// leaves, all at positions 0..~199 — every one of them under ancestor 0.
func TestIndependentManyLeavesUnderOneAncestorWalkTheRootPathOnce(t *testing.T) {
	cfg := defaultConfig()
	cfg.Stride = 2
	f := build(t, 1000, cfg, true)
	levels, s := geometry(cfg)
	access := idsOfType(t, f, "access")

	if g := independentG(t, f, access, levels, s); g != 1 {
		t.Fatalf("setup: %d access records span %d ancestors, want 1 (span is %d leaves)",
			len(access), g, uint64(1)<<(levels-s))
	}
	res, err := f.sys.Verifier.VerifyBatch(context.Background(), &f.token, access)
	if err != nil || !res.Accepted {
		t.Fatalf("honest batch rejected: %v", err)
	}
	if res.AncestorPathRecomputations != 1 {
		t.Fatalf("%d leaves under one ancestor walked the root path %d times, want 1",
			len(access), res.AncestorPathRecomputations)
	}
	// Without reuse the root path would cost n*s hashes; with it, s.
	n := uint64(len(access))
	withReuse := n*(levels-s) + 1*s
	withoutReuse := n*(levels-s) + n*s
	if res.HashCount != withReuse {
		t.Fatalf("HashCount=%d, want %d", res.HashCount, withReuse)
	}
	t.Logf("n=%d leaves, g=1: %d hashes with certificate reuse vs %d without (saved %d)",
		n, withReuse, withoutReuse, withoutReuse-withReuse)
}

// mktree must agree: the covering-ancestor rule the verifier uses is
// leaf_index >> (L - s), so the independent g above is computing the right
// thing.
func TestIndependentCoveringAncestorRule(t *testing.T) {
	for _, s := range []uint64{1, 2, 4, 8} {
		const L = 10
		span := uint64(1) << (L - s)
		for leaf := uint64(0); leaf < 1024; leaf += 37 {
			if got, want := mktree.CoveringAncestorIndex(leaf, L, s), leaf/span; got != want {
				t.Fatalf("s=%d leaf=%d: ancestor %d, want leaf/2^(L-s) = %d", s, leaf, got, want)
			}
		}
	}
}
