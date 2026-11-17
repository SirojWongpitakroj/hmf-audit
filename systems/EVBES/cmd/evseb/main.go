// Command evseb runs the EVSEB prototype.
//
//	evseb -mode demo    Phase 1-6 end to end on a small synthetic dataset
//	evseb -mode bench   Section 14 experiment harness (Fig. 4-11, Table 3)
//
// -chain memory|hardhat selects the contract backend for the demo and for
// Fig. 4-9. Fig. 10/11 and Table 3 always run on the real chain given by
// EVSEB_ETH_RPC_URL / EVSEB_EVM_PRIVATE_KEY_HEX, and are skipped if unset.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"evseb/internal/blockchain"
	"evseb/internal/bloom"
	"evseb/internal/logstore"
	"evseb/internal/search"
	"evseb/internal/trapdoor"
	"evseb/internal/tree"
)

const allExperiments = "fig4,fig5,fig6,fig7,fig8,fig9,fig10,fig11,table3"

func main() {
	mode := flag.String("mode", "demo", "demo | bench")
	chainFlag := flag.String("chain", "memory", "contract backend for demo and fig4-9: memory | hardhat")
	exps := flag.String("exp", allExperiments, "bench: experiments to run")
	reps := flag.Int("reps", 5, "bench: repetitions per point")
	seed := flag.Uint64("seed", 1, "synthetic data seed")
	sizes := flag.String("sizes-kb", "200,400,800,1600,3200", "fig4: file sizes in KB")
	kws := flag.String("keywords", "20,40,60,80,100", "fig5/fig6: keyword counts")
	files := flag.String("files", "1000,2000,3000,4000,5000", "fig7/8/9: dataset sizes")
	fileBytes := flag.Int("file-bytes", 1024, "fig7-9: content bytes per synthetic file (UNSPECIFIED BY PAPER; prototype choice)")
	fig6KB := flag.Int("fig6-file-kb", 3200, "fig6: fixed file size in KB (paper: 3200)")
	fig6Files := flag.Int("fig6-files", 30, "fig6: number of files (UNSPECIFIED BY PAPER; prototype choice)")
	fig7kw := flag.Int("fig7-keywords", 100, "fig7: matching keywords per query")
	op := flag.String("query-op", search.OpAND, "fig6/7: Boolean operator AND|OR|NOT (NOT SPECIFIED BY EVSEB; prototype choice)")
	tg := flag.Int("tampered-global", 100, "fig8: tampered files per dataset")
	match := flag.Int("matching", 20, "fig9: matching files")
	ti := flag.Int("tampered-individual", 10, "fig9: tampered matching files")
	reqs := flag.String("requests", "10,20,30,40,50,60,70,80,90,100", "fig10/11: access requests")
	bloomN := flag.Int("bloom-n", 1000, "expected distinct keywords per sub-node (B3b)")
	bloomP := flag.Float64("bloom-fp", 0.01, "target Bloom false-positive rate (B3b)")
	demoN := flag.Int("demo-logs", 30, "demo: number of synthetic logs")
	flag.Parse()

	m, k := bloom.Params(*bloomN, *bloomP)
	cc, err := chainFromFlag(*chainFlag)
	if err == nil {
		switch *mode {
		case "demo":
			err = runDemo(os.Stdout, *demoN, *seed, m, k, cc)
		case "bench":
			c := benchConfig{reps: *reps, seed: *seed, datasetFileBytes: *fileBytes, fig6FileKB: *fig6KB, fig6Files: *fig6Files,
				fig7Keywords: *fig7kw, queryOp: *op, tamperedGlobal: *tg, matching: *match,
				tamperedIndividual: *ti, bloomM: m, bloomK: k, chain: cc}
			if rpc, key, ok := blockchain.FromEnv(); ok {
				c.realChain = chainConfig{rpcURL: rpc, keyHex: key}
			}
			err = errors.Join(parseInts(*sizes, &c.fileSizesKB), parseInts(*kws, &c.keywordCounts),
				parseInts(*files, &c.datasetSizes), parseInts(*reqs, &c.requests))
			if err == nil {
				err = runBench(os.Stdout, c, strings.Split(*exps, ","))
			}
		default:
			err = fmt.Errorf("unknown mode %q", *mode)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "evseb:", err)
		os.Exit(1)
	}
}

func parseInts(s string, out *[]int) error {
	for _, f := range strings.Split(s, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || v < 1 {
			return fmt.Errorf("bad list %q", s)
		}
		*out = append(*out, v)
	}
	return nil
}

// demoLogs is the demo corpus: synthetic logs, with an explicit DENY line
// added to every sixth one.
func demoLogs(n int, seed uint64) []logstore.Log {
	g := newGenerator(seed)
	out := make([]logstore.Log, n)
	for i := range n {
		lg := g.log(i, 256, int64(1758240000+i))
		if i%6 == 0 {
			lg.Content = "DENY inbound connection blocked by policy\n" + lg.Content
		}
		out[i] = lg
	}
	return out
}

// runDemo walks Phase 1-6 and prints each outcome.
func runDemo(w io.Writer, n int, seed uint64, bloomM, bloomK int, cc chainConfig) error {
	sys, err := newSystem(bloomM, bloomK, time.Now, cc)
	if err != nil {
		return err
	}
	defer sys.close()
	fmt.Fprintln(w, "Phase 1.1 keys: Kenc, Kindex, Kauth, Ksearch = Rand(256)")
	fmt.Fprintf(w, "Phase 1.2 contracts: VerifyAccess, VerifyIntegrity deployed (%s)\n", cc.name())

	ra := sys.registrar(time.Hour, func(u, c string) bool { return u == "analyst" && c == "demo-credential" })
	perms := []string{"Firewall Logs"}
	st, tok, err := ra.RegisterUser("analyst", "demo-credential", perms, "Firewall Logs")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Phase 1.3 Register_User(analyst): %s; Tokenu issued by the registration authority (Eq. 7)\n", st)
	st, _, _ = ra.RegisterUser("mallory", "guess", perms, "Firewall Logs")
	fmt.Fprintf(w, "Phase 1.3 Register_User(mallory): %s\n", st)

	fmt.Fprintf(w, "Phase 2   treeSetup: %d primary nodes, Bloom m=%d k=%d\n", len(sys.tree.PrimaryNodes), bloomM, bloomK)
	for _, lg := range demoLogs(n, seed) {
		if _, err := sys.ingest(lg); err != nil {
			return err
		}
	}
	root, err := sys.anchor()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Phase 3-4 ingested %d logs; Merkle root R=%x anchored via updateMerkleRoot\n", n, root[:8])
	ei, err := tree.Encrypt(sys.tree, sys.keys.Kindex)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Phase 4   Eq. 17 index ciphertexts: CT_T=%dB CT_BF=%dB CT_Inv=%dB\n", len(ei.CTT), len(ei.CTBF), len(ei.CTInv))

	eng := sys.engine()
	tw := func(words ...string) []trapdoor.Trapdoor {
		var out []trapdoor.Trapdoor
		for _, x := range words {
			out = append(out, trapdoor.Generate(x, sys.keys.Ksearch))
		}
		return out
	}
	fmt.Fprintln(w, "Phase 5   Tw = HMAC-SHA256(Ksearch, SHA-256(w)) for each query keyword")
	show := func(label string, q search.Query) (search.Result, error) {
		r, err := eng.SecureQueryExecution(q)
		if errors.Is(err, search.ErrAccessDenied) || errors.Is(err, search.ErrNoMatch) {
			fmt.Fprintf(w, "Phase 6   %s: %s\n", label, err)
			return r, nil
		}
		if err != nil {
			return r, err
		}
		var lids, srcs []string
		for _, e := range r.Entries {
			lids = append(lids, e.LID)
			lg, err := logstore.DecryptLog(e, sys.keys.Kenc) // DU side, Kenc via B18
			if err != nil {
				return r, err
			}
			if !slices.Contains(srcs, lg.Si) {
				srcs = append(srcs, lg.Si)
			}
		}
		slices.Sort(srcs)
		fmt.Fprintf(w, "Phase 6   %s: %d encrypted results, Integrity=%v sources=%v %v\n", label, len(r.Entries), r.Integrity, srcs, lids)
		return r, nil
	}
	q := search.Query{Tw: tw("deny"), LType: "Firewall Logs", Tokenu: tok}
	r, err := show("search deny", q)
	if err != nil {
		return err
	}
	if _, err := show("search deny AND inbound", search.Query{Tw: tw("deny", "inbound"), LType: "Firewall Logs",
		Tokenu: tok, BooleanOperator: search.OpAND}); err != nil {
		return err
	}
	if len(r.Entries) > 0 {
		sys.tamper(r.Entries[0].LID)
		fmt.Fprintf(w, "          tampered ciphertext of %s in cloud storage\n", r.Entries[0].LID)
		if _, err := show("search deny after tamper", q); err != nil {
			return err
		}
	}
	q.BooleanOperator = search.OpNOT
	if _, err := show("search NOT deny", q); err != nil {
		return err
	}
	q.BooleanOperator = ""
	q.LType = "IDPS Logs"
	if _, err := show("search deny in IDPS Logs", q); err != nil {
		return err
	}
	q.LType = "Firewall Logs"
	q.Tw = tw("nonexistentword")
	_, err = show("search nonexistentword", q)
	return err
}
