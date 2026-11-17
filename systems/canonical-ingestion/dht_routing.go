package canonicalingestion

import (
	"encoding/hex"
	"fmt"
	"math/bits"
	"time"
)

const DefaultDHTPeerRTT = 387 * time.Microsecond

// ValidateDHTPeerRTT bounds an audit-only simulation parameter.
func ValidateDHTPeerRTT(rtt time.Duration) error {
	if rtt < 0 || rtt > time.Second {
		return fmt.Errorf("logical peer RTT must be between zero and one second")
	}
	return nil
}

// ValidateLogicalDHTPeerRTT rejects reports from another delay configuration
// and checks that every logical hop, even on a proof-cache hit, was delayed.
func ValidateLogicalDHTPeerRTT(m AuditMetrics, rtt time.Duration) error {
	if err := ValidateDHTPeerRTT(rtt); err != nil {
		return err
	}
	if m.DHTPeerRTT != rtt {
		return fmt.Errorf("DHT report differs from configured simulated peer RTT")
	}
	expected := m.LogicalRoutingSteps
	if rtt == 0 {
		expected = 0
	}
	if m.DHTDelayedHops != expected || (rtt > 0 && expected > uint64((1<<63-1)/rtt)) ||
		m.DHTModeledPeerDelay != time.Duration(expected)*rtt || m.DHTObservedPeerDelay < m.DHTModeledPeerDelay {
		return fmt.Errorf("DHT simulated peer RTT accounting differs from logical hops")
	}
	return nil
}

// LogicalDHTHops validates the bounded XOR routing model. Node count is an
// independent experiment parameter, never derived from observation count.
func LogicalDHTHops(nodes int) (int, error) {
	if nodes < 1 || nodes > 65536 || nodes&(nodes-1) != 0 {
		return 0, fmt.Errorf("logical DHT nodes must be a power of two between 1 and 65536")
	}
	return bits.Len(uint(nodes - 1)), nil
}

// ValidateLogicalDHTMetrics prevents results from silently using another node
// count, truncated owner counters or a routing model outside the measured path.
func ValidateLogicalDHTMetrics(m AuditMetrics, nodes int) error {
	hops, err := LogicalDHTHops(nodes)
	if err != nil {
		return err
	}
	if m.DHTLookupMode != "logical-cassandra-routing-model-v1" || m.DHTLogicalNodes != nodes || m.DHTRoutingHopsPerLookup != hops || len(m.DHTOwnerLookups) != nodes {
		return fmt.Errorf("DHT report does not match the configured logical routing model (%d nodes, %d hops)", nodes, hops)
	}
	id, err := hex.DecodeString(m.DHTRoutingModelIdentity)
	if err != nil || len(id) != 32 {
		return fmt.Errorf("DHT report lacks its bound audit-routing identity")
	}
	var owned uint64
	for _, n := range m.DHTOwnerLookups {
		owned += n
	}
	if owned != m.DHTLookups || m.LogicalRoutingSteps != uint64(hops)*m.DHTLookups || m.CassandraRoutingReads != m.LogicalRoutingSteps {
		return fmt.Errorf("DHT lookup/owner/routing-read accounting differs from the configured logical model")
	}
	return nil
}
