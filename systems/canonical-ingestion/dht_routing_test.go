package canonicalingestion

import (
	"strings"
	"testing"
	"time"
)

func TestLogicalDHTRoutingPolicyAndAccounting(t *testing.T) {
	for nodes, hops := range map[int]int{1: 0, 4: 2, 16: 4, 1024: 10, 65536: 16} {
		if got, err := LogicalDHTHops(nodes); err != nil || got != hops {
			t.Fatal(nodes, got, err)
		}
	}
	for _, nodes := range []int{0, -1, 3, 1023, 65537} {
		if _, err := LogicalDHTHops(nodes); err == nil {
			t.Fatal("invalid topology accepted", nodes)
		}
	}
	m := AuditMetrics{DHTLookupMode: "logical-cassandra-routing-model-v1", DHTLogicalNodes: 1024, DHTRoutingHopsPerLookup: 10, DHTRoutingModelIdentity: strings.Repeat("ab", 32), DHTLookups: 3, LogicalRoutingSteps: 30, CassandraRoutingReads: 30, DHTOwnerLookups: make([]uint64, 1024)}
	m.DHTOwnerLookups[1023] = 3
	if err := ValidateLogicalDHTMetrics(m, 1024); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*AuditMetrics){
		"missing high owners":       func(m *AuditMetrics) { m.DHTOwnerLookups = m.DHTOwnerLookups[:4] },
		"old two-hop accounting":    func(m *AuditMetrics) { m.LogicalRoutingSteps = 6; m.CassandraRoutingReads = 6 },
		"wrong node count":          func(m *AuditMetrics) { m.DHTLogicalNodes = 4 },
		"missing model identity":    func(m *AuditMetrics) { m.DHTRoutingModelIdentity = "" },
		"missing owner attribution": func(m *AuditMetrics) { m.DHTLookups = 4 },
		"wrong hop count":           func(m *AuditMetrics) { m.DHTRoutingHopsPerLookup = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := m
			change(&bad)
			if ValidateLogicalDHTMetrics(bad, 1024) == nil {
				t.Fatal("invalid model report accepted")
			}
		})
	}
}

func TestLogicalPeerRTTAccounting(t *testing.T) {
	m := AuditMetrics{DHTPeerRTT: DefaultDHTPeerRTT, LogicalRoutingSteps: 30, DHTDelayedHops: 30,
		DHTModeledPeerDelay: 30 * DefaultDHTPeerRTT, DHTObservedPeerDelay: 30*DefaultDHTPeerRTT + time.Millisecond}
	if err := ValidateLogicalDHTPeerRTT(m, DefaultDHTPeerRTT); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*AuditMetrics){
		"missing delay": func(m *AuditMetrics) { m.DHTPeerRTT = 0 },
		"skipped hop":   func(m *AuditMetrics) { m.DHTDelayedHops-- },
		"wrong sum":     func(m *AuditMetrics) { m.DHTModeledPeerDelay-- },
		"short wait":    func(m *AuditMetrics) { m.DHTObservedPeerDelay = m.DHTModeledPeerDelay - 1 },
		"overflow":      func(m *AuditMetrics) { m.DHTDelayedHops = 1 << 63; m.LogicalRoutingSteps = 1 << 63 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := m
			change(&bad)
			if ValidateLogicalDHTPeerRTT(bad, DefaultDHTPeerRTT) == nil {
				t.Fatal("invalid delay report accepted")
			}
		})
	}
	if err := ValidateLogicalDHTPeerRTT(AuditMetrics{LogicalRoutingSteps: 30}, 0); err != nil {
		t.Fatal(err)
	}
	for _, rtt := range []time.Duration{-1, time.Second + 1} {
		if ValidateDHTPeerRTT(rtt) == nil {
			t.Fatal("invalid RTT accepted")
		}
	}
}
