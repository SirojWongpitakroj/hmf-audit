package main

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"evseb/internal/logstore"
	"evseb/internal/tree"
)

// Synthetic log generator (Section 14). The paper does not specify its
// original log corpus, so the reproduction uses this deterministic synthetic
// corpus (Q5 ruling); it is not presented as the paper's dataset.

// vocabulary has 128 alphabetic words, so queries of up to 100 distinct
// keywords that occur in the corpus (Fig. 6/7) can be formed.
var vocabulary = strings.Fields(`deny allow drop accept inbound outbound tcp udp icmp alert scan
port connection session login failed success user admin request response error warning
timeout reset established closed blocked policy rule gateway server client host packet
traffic intrusion signature malware exploit attempt brute force ssh http https dns query
certificate handshake upload download redirect proxy cache worker thread memory heap
exception stack trace database transaction commit rollback latency throughput
firewall filter zone interface vlan route nat translation tunnel vpn ipsec tls
cipher token account password lockout privilege escalation audit syslog kernel daemon
process service restart shutdown startup config update patch version module plugin
servlet container deploy undeploy cluster node replica leader follower heartbeat quota
limit throttle queue buffer overflow underflow checksum mismatch corrupt integrity
anomaly baseline threshold severity critical`)

var sources = map[string][]string{
	"Firewall Logs":           {"fw01", "fw02"},
	"IDPS Logs":               {"snort01", "suricata01"},
	"Application Server Logs": {"tomcat01", "nodejs01"},
}

type generator struct{ r *rand.Rand }

func newGenerator(seed uint64) *generator {
	return &generator{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// log returns a synthetic entry of sizeBytes bytes of content. LType cycles
// through the Alg. 1 seed list by i; Si is a random source of that type.
func (g *generator) log(i, sizeBytes int, ti int64) logstore.Log {
	lt := tree.SeedLTypes[i%len(tree.SeedLTypes)]
	src := sources[lt][g.r.IntN(len(sources[lt]))]
	var b strings.Builder
	for b.Len() < sizeBytes {
		fmt.Fprintf(&b, "%d %s %s %s src=10.%d.%d.%d port=%d %s\n", ti, src,
			vocabulary[g.r.IntN(len(vocabulary))], vocabulary[g.r.IntN(len(vocabulary))],
			g.r.IntN(256), g.r.IntN(256), g.r.IntN(256), g.r.IntN(65536),
			vocabulary[g.r.IntN(len(vocabulary))])
	}
	c := b.String()
	if len(c) > sizeBytes {
		c = c[:sizeBytes]
	}
	return logstore.Log{Content: c, Ti: ti, Si: src, LType: lt}
}

// keywords returns n distinct alphabetic keywords (Fig. 5: trapdoor input;
// they need not occur in any log).
func keywords(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = vocabulary[i%len(vocabulary)] + alpha(i/len(vocabulary))
	}
	return out
}

// corpusKeywords returns the first n vocabulary words: keywords that occur in
// the synthetic corpus (Fig. 6/7 "matching keywords"). n <= len(vocabulary).
func corpusKeywords(n int) []string {
	return vocabulary[:min(n, len(vocabulary))]
}

func alpha(n int) string {
	if n == 0 {
		return ""
	}
	var s []byte
	for n > 0 {
		s = append([]byte{byte('a' + (n-1)%26)}, s...)
		n = (n - 1) / 26
	}
	return string(s)
}

// pick returns k distinct indices from [0, n).
func (g *generator) pick(n, k int) []int {
	return g.r.Perm(n)[:k]
}
