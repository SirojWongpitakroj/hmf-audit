package search

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"evseb/internal/access"
	"evseb/internal/blockchain"
	"evseb/internal/crypto"
	"evseb/internal/integrity"
	"evseb/internal/keyword"
	"evseb/internal/logstore"
	"evseb/internal/trapdoor"
	"evseb/internal/tree"
)

var now = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)

type env struct {
	keys crypto.SystemKeys
	eng  *Engine
	ra   *access.Registrar
	tok  []byte            // alice: Firewall Logs only
	lids map[string]string // content -> LID
}

// corpus: two Firewall sources (fw01, fw02) so Q8's cross-sub-node union is
// exercised.
var corpus = []struct{ si, lt, c string }{
	{"fw01", "Firewall Logs", "DENY inbound tcp from 10.0.0.1"},
	{"fw01", "Firewall Logs", "ALLOW outbound tcp to 10.0.0.2"},
	{"fw02", "Firewall Logs", "DENY inbound udp from 10.0.0.3"},
	{"fw02", "Firewall Logs", "ALLOW inbound icmp echo"},
	{"ids01", "IDPS Logs", "ALERT port scan detected tcp"},
}

func setup(t *testing.T) *env {
	t.Helper()
	keys, _ := crypto.GenerateKeys()
	chain := blockchain.NewMemory(keys.Kauth, keys.Kindex, func() time.Time { return now })
	e := &env{keys: keys, lids: map[string]string{}, eng: &Engine{
		Tree: tree.TreeSetup(2048, 5), Chain: chain, Storage: logstore.NewStorage(),
	}}
	e.ra = &access.Registrar{Chain: chain, Kindex: keys.Kindex, Kauth: keys.Kauth, Validity: time.Hour,
		Now: func() time.Time { return now }, VerifyCredentials: func(u, c string) bool { return true }}
	var err error
	if _, e.tok, err = e.ra.RegisterUser("alice", "x", []string{"Firewall Logs"}, "Firewall Logs"); err != nil {
		t.Fatal(err)
	}
	dht := integrity.NewDHT()
	var a logstore.LIDAssigner
	var all []string
	for _, x := range corpus {
		lg := logstore.Log{LID: a.Next(), Content: x.c, Ti: now.Unix(), Si: x.si, LType: x.lt}
		enc, err := logstore.EncryptLog(lg, keys.Kenc)
		if err != nil {
			t.Fatal(err)
		}
		e.eng.Storage.Put(enc)
		dht.Put(lg.LID, integrity.HashLog(enc.CTLog))
		tree.InsertIndices(e.eng.Tree, lg.LType, lg, keyword.Extract(lg.Content), keys.Ksearch)
		e.lids[x.c] = lg.LID
		all = append(all, lg.LID)
	}
	if e.eng.Stored, err = integrity.AnchorRoot(all, dht, chain); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) query(op string, words ...string) Query {
	var tws []trapdoor.Trapdoor
	for _, w := range words {
		tws = append(tws, trapdoor.Generate(w, e.keys.Ksearch))
	}
	return Query{Tw: tws, LType: "Firewall Logs", Tokenu: e.tok, BooleanOperator: op}
}

func (e *env) ids(contents ...string) []string {
	var out []string
	for _, c := range contents {
		out = append(out, e.lids[c])
	}
	slices.Sort(out)
	return out
}

func lidsOf(r Result) []string {
	var out []string
	for _, x := range r.Entries {
		out = append(out, x.LID)
	}
	return out
}

func (e *env) run(t *testing.T, q Query) Result {
	t.Helper()
	r, err := e.eng.SecureQueryExecution(q)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Q8: "deny" occurs under fw01 and fw02; both are returned.
func TestSingleKeywordAcrossSubNodes(t *testing.T) {
	e := setup(t)
	r := e.run(t, e.query("", "deny"))
	want := e.ids(corpus[0].c, corpus[2].c)
	if !slices.Equal(lidsOf(r), want) || !r.Integrity {
		t.Fatalf("got %v integrity=%v, want %v", lidsOf(r), r.Integrity, want)
	}
	for _, x := range r.Entries { // FR-20: results are ciphertext
		if strings.Contains(string(x.CTLog), "DENY") {
			t.Fatal("plaintext returned")
		}
		lg, err := logstore.DecryptLog(x, e.keys.Kenc)
		if err != nil || !strings.Contains(lg.Content, "DENY") {
			t.Fatalf("returned entry does not decrypt to a match: %v", err)
		}
	}
}

// B9: multi-keyword AND / OR / NOT from one query.
func TestMultiKeywordBoolean(t *testing.T) {
	e := setup(t)
	if got := lidsOf(e.run(t, e.query(OpAND, "deny", "inbound"))); !slices.Equal(got, e.ids(corpus[0].c, corpus[2].c)) {
		t.Fatalf("deny AND inbound = %v", got)
	}
	if got := lidsOf(e.run(t, e.query(OpAND, "deny", "tcp"))); !slices.Equal(got, e.ids(corpus[0].c)) {
		t.Fatalf("deny AND tcp = %v", got)
	}
	if got := lidsOf(e.run(t, e.query(OpOR, "udp", "icmp"))); !slices.Equal(got, e.ids(corpus[2].c, corpus[3].c)) {
		t.Fatalf("udp OR icmp = %v", got)
	}
	// AND whose keywords all have candidates but share no log: an empty
	// result, not "no match" (Q12 item 4).
	if r := e.run(t, e.query(OpAND, "deny", "icmp")); len(r.Entries) != 0 || !r.Integrity {
		t.Fatalf("deny AND icmp = %v", lidsOf(r))
	}
	// OR with one absent keyword still returns the other's matches.
	if got := lidsOf(e.run(t, e.query(OpOR, "nothere", "icmp"))); !slices.Equal(got, e.ids(corpus[3].c)) {
		t.Fatalf("nothere OR icmp = %v", got)
	}
}

// Q8: Lall for NOT is every LID under the Firewall primary node.
func TestNOTQuery(t *testing.T) {
	e := setup(t)
	if got := lidsOf(e.run(t, e.query(OpNOT, "deny"))); !slices.Equal(got, e.ids(corpus[1].c, corpus[3].c)) {
		t.Fatalf("NOT deny = %v", got)
	}
	// Q12 item 4: NOT of a keyword with no candidate is Lall, not "no match".
	all := e.ids(corpus[0].c, corpus[1].c, corpus[2].c, corpus[3].c)
	if r := e.run(t, e.query(OpNOT, "nothere")); !slices.Equal(lidsOf(r), all) || !r.Integrity {
		t.Fatalf("NOT nothere = %v, want Lall %v", lidsOf(r), all)
	}
	// "alert" occurs only under IDPS Logs: no Firewall candidate either.
	if got := lidsOf(e.run(t, e.query(OpNOT, "alert"))); !slices.Equal(got, all) {
		t.Fatalf("NOT alert = %v, want Lall", got)
	}
}

// Q12 item 3: Eq. 27 is unary. NOT with other than one trapdoor is rejected,
// never evaluated as Lall \ (L1 ∪ ... ∪ Lk).
func TestNOTRejectsMultipleTrapdoors(t *testing.T) {
	e := setup(t)
	for _, words := range [][]string{{"deny", "icmp"}, {"deny", "nothere", "udp"}, {"nothere", "alsonot"}} {
		if _, err := e.eng.SecureQueryExecution(e.query(OpNOT, words...)); !errors.Is(err, ErrNOTArity) {
			t.Fatalf("NOT %v: want ErrNOTArity, got %v", words, err)
		}
	}
	// Access is still checked first (Alg. 3 line 5).
	q := e.query(OpNOT, "deny", "icmp")
	q.LType = "IDPS Logs"
	if _, err := e.eng.SecureQueryExecution(q); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("unpermitted multi-keyword NOT: got %v", err)
	}
}

func TestAccessDenied(t *testing.T) {
	e := setup(t)
	q := e.query("", "alert")
	q.LType = "IDPS Logs" // token only permits Firewall Logs
	if _, err := e.eng.SecureQueryExecution(q); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("want Access Denied, got %v", err)
	}
	e.ra.Validity = -time.Second
	_, expired, _ := e.ra.RegisterUser("bob", "x", []string{"Firewall Logs"}, "Firewall Logs")
	q = e.query("", "deny")
	q.Tokenu = expired
	if _, err := e.eng.SecureQueryExecution(q); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("expired token: got %v", err)
	}
	q.Tokenu = []byte("forged-token-bytes-forged-token-bytes")
	if _, err := e.eng.SecureQueryExecution(q); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("forged token: got %v", err)
	}
}

func TestNoMatch(t *testing.T) {
	e := setup(t)
	if _, err := e.eng.SecureQueryExecution(e.query("", "nonexistent")); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("want No matching records found, got %v", err)
	}
	if _, err := e.eng.SecureQueryExecution(e.query(OpOR, "nonexistent", "alsonot")); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("all-absent OR: got %v", err)
	}
	// Q12 item 4: AND with one keyword absent is "no match", even when the
	// others have candidates, in any position.
	for _, words := range [][]string{{"deny", "inbound", "nothere"}, {"nothere", "deny"}} {
		if _, err := e.eng.SecureQueryExecution(e.query(OpAND, words...)); !errors.Is(err, ErrNoMatch) {
			t.Fatalf("AND %v: want No matching records found, got %v", words, err)
		}
	}
	// A keyword present only under another LType has no candidate here.
	if _, err := e.eng.SecureQueryExecution(e.query(OpAND, "deny", "alert")); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("AND with other-LType keyword: got %v", err)
	}
	q := e.query("", "deny")
	other, _ := crypto.Rand256()
	q.Tw = []trapdoor.Trapdoor{trapdoor.Generate("deny", other)}
	if _, err := e.eng.SecureQueryExecution(q); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("foreign-key trapdoor: got %v", err)
	}
	q.Tw = nil
	if _, err := e.eng.SecureQueryExecution(q); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("empty trapdoor list: got %v", err)
	}
}

func TestTamperedResultExcluded(t *testing.T) {
	e := setup(t)
	lid := e.lids[corpus[2].c]
	enc, _ := e.eng.Storage.Get(lid)
	enc.CTLog[20] ^= 1
	e.eng.Storage.Put(enc)
	r := e.run(t, e.query("", "deny"))
	if r.Integrity || !slices.Equal(lidsOf(r), e.ids(corpus[0].c)) {
		t.Fatalf("tampered entry not excluded: %v integrity=%v", lidsOf(r), r.Integrity)
	}
}

func TestBooleanSetOperationsEq25to27(t *testing.T) {
	l1 := []string{"1", "2", "3"}
	l2 := []string{"2", "3", "4"}
	l3 := []string{"3", "4", "5"}
	all := []string{"1", "2", "3", "4", "5", "6"}
	cases := []struct {
		op   string
		sets [][]string
		want []string
	}{
		{OpAND, [][]string{l1, l2}, []string{"2", "3"}},
		{OpAND, [][]string{l1, l2, l3}, []string{"3"}},
		{OpOR, [][]string{l1, l2}, []string{"1", "2", "3", "4"}},
		{OpOR, [][]string{l1, l3}, []string{"1", "2", "3", "4", "5"}},
		{OpNOT, [][]string{l1}, []string{"4", "5", "6"}},
		{OpNOT, [][]string{{}}, all},
		{"", [][]string{{"3", "1", "1"}}, []string{"1", "3"}},
		{OpAND, [][]string{l1}, l1},
	}
	for _, c := range cases {
		if got, err := PerformBooleanOperations(c.sets, c.op, all); err != nil || !slices.Equal(got, c.want) {
			t.Fatalf("%s %v = %v (%v), want %v", c.op, c.sets, got, err, c.want)
		}
	}
	if _, err := PerformBooleanOperations(nil, OpAND, all); err == nil {
		t.Fatal("no operand sets accepted")
	}
	if _, err := PerformBooleanOperations([][]string{l1, l3}, OpNOT, all); !errors.Is(err, ErrNOTArity) {
		t.Fatalf("binary NOT: want ErrNOTArity, got %v", err)
	}
}

func TestNoMatchRulePerOperator(t *testing.T) {
	some, none := []string{"1"}, []string(nil)
	cases := []struct {
		op   string
		sets [][]string
		want bool
	}{
		{OpAND, [][]string{some, some}, false},
		{OpAND, [][]string{some, none}, true},
		{OpAND, [][]string{none, some, some}, true},
		{OpOR, [][]string{none, some}, false},
		{OpOR, [][]string{none, none}, true},
		{"", [][]string{none}, true},
		{"", [][]string{some}, false},
		{OpNOT, [][]string{none}, false},
		{OpNOT, [][]string{some}, false},
	}
	for _, c := range cases {
		if got := noMatch(c.sets, c.op); got != c.want {
			t.Fatalf("noMatch(%q, %v) = %v, want %v", c.op, c.sets, got, c.want)
		}
	}
}
