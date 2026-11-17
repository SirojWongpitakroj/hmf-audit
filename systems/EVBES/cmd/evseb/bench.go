package main

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"evseb/internal/access"
	"evseb/internal/blockchain"
	"evseb/internal/bloom"
	"evseb/internal/crypto"
	"evseb/internal/integrity"
	"evseb/internal/logstore"
	"evseb/internal/search"
	"evseb/internal/trapdoor"
)

// benchConfig holds the Section 14 sweep parameters. Defaults are the
// paper's Sec. VI.C values (Q4 ruling). -file-bytes, -fig6-files and
// -query-op cover what the paper leaves unstated (prototype choices, Q12).
type benchConfig struct {
	reps               int
	seed               uint64
	fileSizesKB        []int  // Fig. 4
	keywordCounts      []int  // Fig. 5, Fig. 6
	datasetSizes       []int  // Fig. 7, 8, 9
	datasetFileBytes   int    // Fig. 7-9: content size of each dataset file (paper: unstated)
	fig6FileKB         int    // Fig. 6: "fixed file size of 3200 KB" (Q12 item 2)
	fig6Files          int    // Fig. 6: number of files (UNSPECIFIED BY PAPER; prototype choice)
	fig7Keywords       int    // Fig. 7: 100 matching keywords
	queryOp            string // Fig. 6/7 operator (NOT SPECIFIED BY EVSEB; prototype choice)
	tamperedGlobal     int    // Fig. 8: 100 tampered per dataset
	matching           int    // Fig. 9: 20 matching files
	tamperedIndividual int    // Fig. 9: 10 of the matching files tampered
	requests           []int  // Fig. 10/11: 10..100 access requests
	bloomM, bloomK     int
	chain              chainConfig // backend for Fig. 4-9
	realChain          chainConfig // chain for Fig. 10/11 and Table 3 (empty: skipped)
}

// row is one measured point.
type row struct {
	exp    string
	x      int
	stats  timing
	detail string
}

type timing struct{ mean, median, min time.Duration }

// minBatch is the shortest wall-clock span one sample may cover. The system
// clock on some hosts (observed on Windows) ticks too coarsely to time a
// sub-millisecond operation directly, so fast operations are run `iters`
// times per sample and the per-operation time is reported.
const minBatch = 10 * time.Millisecond

func measure(reps int, f func() error) (timing, error) {
	iters := 1
	for {
		start := time.Now()
		for range iters {
			if err := f(); err != nil {
				return timing{}, err
			}
		}
		if time.Since(start) >= minBatch {
			break
		}
		iters *= 2
	}
	ds := make([]time.Duration, 0, reps)
	for range reps {
		start := time.Now()
		for range iters {
			if err := f(); err != nil {
				return timing{}, err
			}
		}
		ds = append(ds, time.Since(start)/time.Duration(iters))
	}
	return summarize(ds), nil
}

func summarize(ds []time.Duration) timing {
	ds = slices.Clone(ds)
	slices.Sort(ds)
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return timing{mean: sum / time.Duration(len(ds)), median: ds[len(ds)/2], min: ds[0]}
}

// fig4 is encryption time vs file size: Eq. 14 + Eq. 15 on one log entry.
func fig4(c benchConfig) ([]row, error) {
	g := newGenerator(c.seed)
	keys, err := crypto.GenerateKeys()
	if err != nil {
		return nil, err
	}
	var rows []row
	for _, kb := range c.fileSizesKB {
		lg := g.log(0, kb*1024, 0)
		lg.LID = "000000000000"
		t, err := measure(c.reps, func() error {
			_, err := logstore.EncryptLog(lg, keys.Kenc)
			return err
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{"fig4-encrypt", kb, t, "x=file KB"})
	}
	return rows, nil
}

// fig5 is trapdoor generation time vs number of keywords (Eq. 18-19 per
// keyword).
func fig5(c benchConfig) ([]row, error) {
	keys, err := crypto.GenerateKeys()
	if err != nil {
		return nil, err
	}
	var rows []row
	for _, n := range c.keywordCounts {
		kws := keywords(n)
		t, _ := measure(c.reps, func() error {
			for _, w := range kws {
				_ = trapdoor.Generate(w, keys.Ksearch)
			}
			return nil
		})
		rows = append(rows, row{"fig5-trapdoor", n, t, "x=keywords"})
	}
	return rows, nil
}

// dataset ingests n synthetic files of fileBytes each and anchors the root.
func dataset(c benchConfig, g *generator, n, fileBytes int) (*system, error) {
	sys, err := newSystem(c.bloomM, c.bloomK, time.Now, c.chain)
	if err != nil {
		return nil, err
	}
	for i := range n {
		if _, err := sys.ingest(g.log(i, fileBytes, int64(i))); err != nil {
			sys.close()
			return nil, err
		}
	}
	if _, err = sys.anchor(); err != nil {
		sys.close()
		return nil, err
	}
	return sys, nil
}

// searchPoint times SecureQueryExecution for one multi-keyword query (B9)
// over the "Firewall Logs" primary node, with trapdoors generated beforehand.
// results is the number of returned entries, or "no-match" when Alg. 3
// line 10 applies (Q12 item 4).
func searchPoint(c benchConfig, sys *system, kws []string) (timing, string, error) {
	tok, err := sys.register("analyst", []string{"Firewall Logs"}, time.Hour)
	if err != nil {
		return timing{}, "", err
	}
	q := search.Query{LType: "Firewall Logs", Tokenu: tok, BooleanOperator: c.queryOp}
	for _, w := range kws {
		q.Tw = append(q.Tw, trapdoor.Generate(w, sys.keys.Ksearch))
	}
	eng := sys.engine()
	var results string
	t, err := measure(c.reps, func() error {
		r, err := eng.SecureQueryExecution(q)
		if errors.Is(err, search.ErrNoMatch) {
			results = "no-match"
			return nil
		}
		results = fmt.Sprint(len(r.Entries))
		return err
	})
	return t, results, err
}

// fig6 is search time vs number of keywords with a fixed file size of 3200
// KB (Q12 item 2). The paper does not state how many files Fig. 6 uses, so
// -fig6-files is a separately documented prototype choice. The Boolean
// operator is not specified by EVSEB either (-query-op, Q12 item 1).
func fig6(c benchConfig) ([]row, error) {
	sys, err := dataset(c, newGenerator(c.seed), c.fig6Files, c.fig6FileKB*1024)
	if err != nil {
		return nil, err
	}
	defer sys.close()
	var rows []row
	for _, n := range c.keywordCounts {
		t, results, err := searchPoint(c, sys, corpusKeywords(n))
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{"fig6-search-keywords", n, t,
			fmt.Sprintf("x=keywords op=%s file=%dKB files=%d results=%s", c.queryOp, c.fig6FileKB, len(sys.lids), results)})
	}
	return rows, nil
}

// fig7 is search time vs number of files with a fixed number of matching
// keywords (100).
func fig7(c benchConfig) ([]row, error) {
	g := newGenerator(c.seed)
	var rows []row
	for _, n := range c.datasetSizes {
		sys, err := dataset(c, g, n, c.datasetFileBytes)
		if err != nil {
			return nil, err
		}
		t, results, err := searchPoint(c, sys, corpusKeywords(c.fig7Keywords))
		sys.close()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{"fig7-search-files", n, t,
			fmt.Sprintf("x=files op=%s keywords=%d results=%s", c.queryOp, c.fig7Keywords, results)})
	}
	return rows, nil
}

// fig8 is global integrity verification: Alg. 4.1 over every LID of a
// dataset with c.tamperedGlobal files tampered.
func fig8(c benchConfig) ([]row, error) {
	g := newGenerator(c.seed)
	var rows []row
	for _, n := range c.datasetSizes {
		sys, err := dataset(c, g, n, c.datasetFileBytes)
		if err != nil {
			return nil, err
		}
		k := min(c.tamperedGlobal, n)
		for _, i := range g.pick(n, k) {
			sys.tamper(sys.lids[i])
		}
		var verified []string
		t, err := measure(c.reps, func() error {
			var err error
			verified, _, err = sys.verify(sys.lids)
			return err
		})
		sys.close()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{"fig8-global-verify", n, t,
			fmt.Sprintf("x=files tampered=%d detected=%d", k, n-len(verified))})
	}
	return rows, nil
}

// fig9 is individual-file verification: Alg. 4.1 over a candidate set of
// c.matching LIDs of which c.tamperedIndividual are tampered.
func fig9(c benchConfig) ([]row, error) {
	g := newGenerator(c.seed)
	var rows []row
	for _, n := range c.datasetSizes {
		sys, err := dataset(c, g, n, c.datasetFileBytes)
		if err != nil {
			return nil, err
		}
		m := min(c.matching, n)
		idx := g.pick(n, m)
		slices.Sort(idx)
		cands := make([]string, m)
		for j, i := range idx {
			cands[j] = sys.lids[i]
		}
		k := min(c.tamperedIndividual, m)
		for _, j := range g.pick(m, k) {
			sys.tamper(cands[j])
		}
		var verified []string
		t, err := measure(c.reps, func() error {
			var err error
			verified, _, err = sys.verify(cands)
			return err
		})
		sys.close()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{"fig9-individual-verify", n, t,
			fmt.Sprintf("x=files matching=%d tampered=%d detected=%d", m, k, m-len(verified))})
	}
	return rows, nil
}

// fig10and11 measures verifyAccess throughput (Fig. 10) and latency (Fig. 11)
// for 10..100 access requests, on-chain vs off-chain (QUESTIONS.md Q12
// item 5). On-chain: the N requests are one workload of verifyAccess
// transactions, all submitted before any receipt is awaited, so they can be
// pending together (onChainWorkload). Off-chain: a pre-validated token check
// (decrypt Tokenu with Kauth and check the claims against the policy record
// read once before the loop), with no chain round-trip.
func fig10and11(c benchConfig) ([]row, error) {
	sys, err := newSystem(c.bloomM, c.bloomK, time.Now, c.realChain)
	if err != nil {
		return nil, err
	}
	defer sys.close()
	tok, err := sys.register("analyst", []string{"Firewall Logs"}, time.Hour)
	if err != nil {
		return nil, err
	}
	ct, commitment, _, err := sys.eth.PolicyRecord("analyst")
	if err != nil {
		return nil, err
	}
	offChain := func() error {
		ut, err := access.DecryptToken(tok, sys.keys.Kauth)
		if err != nil {
			return err
		}
		cl, err := access.BuildClaims(ut, ct, sys.keys.Kindex)
		if err != nil {
			return err
		}
		if !access.CheckClaims(cl, commitment, "Firewall Logs", time.Now().Unix()) {
			return fmt.Errorf("off-chain verifyAccess denied a valid token")
		}
		return nil
	}
	var rows []row
	for _, n := range c.requests {
		t, tps, total, blocks, err := onChainWorkload(sys.eth, tok, "Firewall Logs", n)
		if err != nil {
			return nil, fmt.Errorf("onchain: %w", err)
		}
		rows = append(rows, row{"fig10-11-verifyaccess-onchain", n, t,
			fmt.Sprintf("x=requests tps=%.1f total_ms=%.3f blocks=%d", tps, ms(total), blocks)})
		if t, tps, total, err = measureRequests(n, offChain); err != nil {
			return nil, fmt.Errorf("offchain: %w", err)
		}
		rows = append(rows, row{"fig10-11-verifyaccess-offchain", n, t,
			fmt.Sprintf("x=requests tps=%.1f total_ms=%.3f", tps, ms(total))})
	}
	return rows, nil
}

// onChainWorkload submits n verifyAccess transactions back to back (each
// request: decrypt Tokenu, read the policy record, send the claims). No
// submission waits for an earlier one to be mined, so the requests can be
// pending together. Between submissions, receipts that have already appeared
// are recorded without blocking, so a request's latency is not inflated by
// the submissions after it. Latency is each request's submission to its
// receipt; TPS is n over the wall time from the first submission to the last
// receipt. blocks is how many distinct blocks the workload landed in.
// Whether requests share a block depends on the node's mining mode:
// Hardhat's default automine mines each transaction on arrival.
func onChainWorkload(eth *blockchain.Ethereum, tok []byte, lType string, n int) (timing, float64, time.Duration, int, error) {
	nonce, err := eth.PendingNonce()
	if err != nil {
		return timing{}, 0, 0, 0, err
	}
	w := eth.NewReceiptWatcher()
	sent := make([]time.Time, n)
	start := time.Now()
	for i := range n {
		sent[i] = time.Now()
		tx, err := eth.SendVerifyAccess(tok, lType, nonce+uint64(i))
		if err != nil {
			return timing{}, 0, 0, 0, err
		}
		w.Add(tx)
		if err := w.Poll(); err != nil {
			return timing{}, 0, 0, 0, err
		}
	}
	receipts, mined, err := w.Wait()
	if err != nil {
		return timing{}, 0, 0, 0, err
	}
	total := time.Since(start)
	lat := make([]time.Duration, n)
	blocks := map[uint64]bool{}
	for i := range n {
		lat[i] = mined[i].Sub(sent[i])
		blocks[receipts[i].BlockNumber.Uint64()] = true
	}
	return summarize(lat), float64(n) / total.Seconds(), total, len(blocks), nil
}

// measureRequests runs n sequential off-chain requests and reports per-request latency
// and throughput. If the n-request batch spans less than minBatch (clock too
// coarse for microsecond requests), the batch is repeated until it doesn't;
// then only the mean per-request latency is known, and median/min report it
// too. total is the wall time of one n-request batch.
func measureRequests(n int, f func() error) (timing, float64, time.Duration, error) {
	lat := make([]time.Duration, 0, n)
	start := time.Now()
	for range n {
		t0 := time.Now()
		if err := f(); err != nil {
			return timing{}, 0, 0, err
		}
		lat = append(lat, time.Since(t0))
	}
	total := time.Since(start)
	if total >= minBatch {
		return summarize(lat), float64(n) / total.Seconds(), total, nil
	}
	for iters := 2; ; iters *= 2 {
		start = time.Now()
		for range n * iters {
			if err := f(); err != nil {
				return timing{}, 0, 0, err
			}
		}
		if el := time.Since(start); el >= minBatch {
			per := el / time.Duration(n*iters)
			return timing{mean: per, median: per, min: per}, float64(n*iters) / el.Seconds(), per * time.Duration(n), nil
		}
	}
}

// table3 measures gas for the four Table 3 operations on the real chain,
// plus verifyAccess sent as a transaction (not in Table 3). Cost is reported
// in gas units and in ETH at the local chain's effective gas price. No USD
// column is computed: the paper's USD figures imply inconsistent gas prices
// (QUESTIONS.md Q12 item 6).
func table3(c benchConfig, w io.Writer) error {
	sys, err := newSystem(c.bloomM, c.bloomK, time.Now, c.realChain)
	if err != nil {
		return err
	}
	defer sys.close()
	texp := time.Now().Add(time.Hour).Unix()
	ct, err := access.EncryptPolicy(access.NewPolicy("analyst", []string{"Firewall Logs"}, "Firewall Logs", texp), sys.keys.Kindex)
	if err != nil {
		return err
	}
	grant, err := sys.eth.GrantAccessReceipt("analyst", ct)
	if err != nil {
		return err
	}
	g := newGenerator(c.seed)
	for i := range 16 {
		if _, err := sys.ingest(g.log(i, c.datasetFileBytes, int64(i))); err != nil {
			return err
		}
	}
	// R over the dataset's DHT hLIDs; updateMerkleRoot is the only transaction timed here.
	leaves := make([][]byte, len(sys.lids))
	for i, lid := range sys.lids {
		leaves[i], _ = sys.dht.Get(lid)
	}
	upd, err := sys.eth.UpdateMerkleRootReceipt(integrity.Build(sys.lids, leaves).Root)
	if err != nil {
		return err
	}
	tok, err := access.EncryptToken(access.UserToken{UserID: "analyst", Permissions: []string{"Firewall Logs"},
		Texp: texp}, sys.keys.Kauth)
	if err != nil {
		return err
	}
	va, err := sys.eth.VerifyAccessReceipt(tok, "Firewall Logs")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "table3,operation,gas_used,effective_gas_price_wei,cost_eth")
	for _, x := range []struct {
		name string
		r    *types.Receipt
	}{
		{"deploy VerifyAccess", sys.eth.DeployVerifyAccess},
		{"deploy VerifyIntegrity", sys.eth.DeployVerifyIntegrity},
		{"updateMerkleRoot()", upd},
		{"grantAccess()", grant},
		{"verifyAccess() as tx (not in Table 3)", va},
	} {
		fmt.Fprintf(w, "table3,%s,%d,%s,%s\n", x.name, x.r.GasUsed, x.r.EffectiveGasPrice, weiToETH(blockchain.GasCost(x.r)))
	}
	return nil
}

func weiToETH(wei *big.Int) string {
	return new(big.Rat).SetFrac(wei, big.NewInt(1e18)).FloatString(12)
}

// chainExperiments need the real chain.
var chainExperiments = map[string]bool{"fig10": true, "fig11": true, "table3": true}

func runBench(w io.Writer, c benchConfig, exps []string) error {
	fns := map[string]func(benchConfig) ([]row, error){
		"fig4": fig4, "fig5": fig5, "fig6": fig6, "fig7": fig7, "fig8": fig8, "fig9": fig9,
		"fig10": fig10and11, "fig11": fig10and11,
	}
	fmt.Fprintf(w, "# EVSEB harness  reps=%d seed=%d chain(fig4-9)=%s bloom m=%d k=%d (Pfp@1000=%.4f)\n",
		c.reps, c.seed, c.chain.name(), c.bloomM, c.bloomK, bloom.FalsePositiveRate(c.bloomK, 1000, c.bloomM))
	fmt.Fprintf(w, "# prototype choices (not specified by EVSEB): query-op=%s fig6-files=%d file-bytes(fig7-9)=%d\n",
		c.queryOp, c.fig6Files, c.datasetFileBytes)
	fmt.Fprintln(w, "experiment,x,mean_ms,median_ms,min_ms,detail")
	ran10and11 := false
	var skipped []string
	for _, e := range exps {
		if chainExperiments[e] && c.realChain.rpcURL == "" {
			skipped = append(skipped, e)
			continue
		}
		if e == "table3" {
			if err := table3(c, w); err != nil {
				return fmt.Errorf("table3: %w", err)
			}
			continue
		}
		f, ok := fns[e]
		if !ok {
			return fmt.Errorf("unknown experiment %q", e)
		}
		if e == "fig10" || e == "fig11" {
			if ran10and11 {
				continue
			}
			ran10and11 = true
		}
		rows, err := f(c)
		if err != nil {
			return fmt.Errorf("%s: %w", e, err)
		}
		for _, r := range rows {
			fmt.Fprintf(w, "%s,%d,%.4f,%.4f,%.4f,%s\n", r.exp, r.x, ms(r.stats.mean), ms(r.stats.median), ms(r.stats.min), r.detail)
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(w, "# SKIPPED %v: need a running chain (%s and %s)\n", skipped, blockchain.EnvRPCURL, blockchain.EnvPrivateKey)
	}
	return nil
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
