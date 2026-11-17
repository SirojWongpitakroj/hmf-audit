package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"evseb/internal/blockchain"
	"evseb/internal/bloom"
	"evseb/internal/keyword"
)

func smallConfig() benchConfig {
	m, k := bloom.Params(1000, 0.01)
	c := benchConfig{reps: 2, seed: 7, fileSizesKB: []int{1, 4}, keywordCounts: []int{20, 100},
		datasetSizes: []int{50, 120}, datasetFileBytes: 256, fig6FileKB: 64, fig6Files: 6, fig7Keywords: 100, queryOp: "AND",
		tamperedGlobal: 10, matching: 20, tamperedIndividual: 10, requests: []int{2, 4}, bloomM: m, bloomK: k}
	if rpc, key, ok := blockchain.FromEnv(); ok {
		c.realChain = chainConfig{rpcURL: rpc, keyHex: key}
	}
	return c
}

func TestGeneratorSizesAndDeterminism(t *testing.T) {
	a, b := newGenerator(3), newGenerator(3)
	for i := range 10 {
		la, lb := a.log(i, 2048, 0), b.log(i, 2048, 0)
		if la != lb {
			t.Fatal("generator not deterministic")
		}
		if len(la.Content) != 2048 {
			t.Fatalf("content length %d", len(la.Content))
		}
		if len(keyword.Extract(la.Content)) == 0 {
			t.Fatal("synthetic log yields no keywords")
		}
	}
	seen := map[string]bool{}
	for _, w := range keywords(100) {
		if seen[w] {
			t.Fatalf("duplicate keyword %q", w)
		}
		seen[w] = true
	}
	// Fig. 6/7 need 100 distinct corpus keywords that survive B15 selection.
	if len(vocabulary) < 100 {
		t.Fatalf("vocabulary has %d words", len(vocabulary))
	}
	for _, w := range corpusKeywords(100) {
		if seen[w+"x"] || len(keyword.Select([]string{w}, keyword.DefaultMinLength)) != 1 || keyword.Stopwords[w] {
			t.Fatalf("vocabulary word %q is not a selectable keyword", w)
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(vocabulary)))) != len(vocabulary) {
		t.Fatal("vocabulary has duplicates")
	}
}

func TestFig8And9DetectAllTampered(t *testing.T) {
	c := smallConfig()
	rows, err := fig8(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if !strings.Contains(r.detail, "tampered=10 detected=10") {
			t.Fatalf("fig8 row %+v", r)
		}
	}
	if rows, err = fig9(c); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if !strings.Contains(r.detail, "matching=20 tampered=10 detected=10") {
			t.Fatalf("fig9 row %+v", r)
		}
	}
}

func TestFig6And7MultiKeywordSearch(t *testing.T) {
	c := smallConfig()
	c.queryOp = "OR"
	rows, err := fig6(c)
	if err != nil {
		t.Fatal(err)
	}
	// Q12 item 2: the file size is 64 KB here and the file count is its own
	// setting, not derived from a corpus size.
	if len(rows) != 2 || !strings.Contains(rows[0].detail, "op=OR file=64KB files=6") {
		t.Fatalf("fig6 rows %+v", rows)
	}
	for _, r := range rows {
		if strings.Contains(r.detail, "results=0") || strings.Contains(r.detail, "no-match") {
			t.Fatalf("OR over corpus keywords returned nothing: %+v", r)
		}
	}
	// AND over 64 KB files: each file holds every vocabulary word, so every
	// Firewall file (2 of 6) matches.
	c.queryOp = "AND"
	if rows, err = fig6(c); err != nil || !strings.Contains(rows[1].detail, "results=2") {
		t.Fatalf("fig6 AND rows %+v %v", rows, err)
	}
	if rows, err = fig7(c); err != nil || len(rows) != 2 || !strings.Contains(rows[1].detail, "keywords=100") {
		t.Fatalf("fig7 rows %+v %v", rows, err)
	}
}

func TestRunBenchOutput(t *testing.T) {
	var buf bytes.Buffer
	c := smallConfig()
	c.realChain = chainConfig{} // chain experiments must be skipped, not fail
	if err := runBench(&buf, c, strings.Split(allExperiments, ",")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, e := range []string{"fig4-encrypt,1,", "fig5-trapdoor,100,", "fig6-search-keywords,20,",
		"fig7-search-files,120,", "fig8-global-verify,120,", "fig9-individual-verify,50,",
		"# SKIPPED [fig10 fig11 table3]"} {
		if !strings.Contains(out, e) {
			t.Fatalf("missing %q in:\n%s", e, out)
		}
	}
	if err := runBench(&buf, c, []string{"fig99"}); err == nil {
		t.Fatal("unknown experiment accepted")
	}
}

// Needs EVSEB_ETH_RPC_URL / EVSEB_EVM_PRIVATE_KEY_HEX (a running Hardhat node).
func TestChainExperiments(t *testing.T) {
	c := smallConfig()
	if c.realChain.rpcURL == "" {
		t.Skip("no chain configured")
	}
	var buf bytes.Buffer
	if err := runBench(&buf, c, []string{"fig10", "fig11", "table3"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, e := range []string{"fig10-11-verifyaccess-onchain,2,", "fig10-11-verifyaccess-offchain,4,", "blocks=",
		"table3,deploy VerifyAccess,", "table3,deploy VerifyIntegrity,", "table3,updateMerkleRoot(),", "table3,grantAccess(),"} {
		if !strings.Contains(out, e) {
			t.Fatalf("missing %q in:\n%s", e, out)
		}
	}
	if strings.Count(out, "fig10-11-verifyaccess-onchain") != 2 {
		t.Fatal("fig10/fig11 measured twice")
	}
}

// Q8 ruling: "deny" is matched across fw01 and fw02, with the full count.
func TestDemoRuns(t *testing.T) {
	const n, seed = 30, 1
	var want []string
	for i, lg := range demoLogs(n, seed) {
		if lg.LType == "Firewall Logs" && slices.Contains(keyword.Extract(lg.Content), "deny") {
			want = append(want, fmt.Sprintf("%012d", i))
		}
	}
	var buf bytes.Buffer
	m, k := bloom.Params(1000, 0.01)
	if err := runDemo(&buf, n, seed, m, k, chainConfig{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, line := range []string{
		"Register_User(analyst): User Registered Successfully",
		"Register_User(mallory): Authentication Failed",
		fmt.Sprintf("search deny: %d encrypted results, Integrity=true sources=[fw01 fw02] %v", len(want), want),
		fmt.Sprintf("search deny after tamper: %d encrypted results, Integrity=false", len(want)-1),
		"search deny in IDPS Logs: Access Denied",
		"search nonexistentword: No matching records found",
	} {
		if !strings.Contains(out, line) {
			t.Fatalf("demo output missing %q:\n%s", line, out)
		}
	}
	t.Log("\n" + out)
}
