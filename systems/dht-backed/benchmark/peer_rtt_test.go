package benchmark

import (
	"context"
	"testing"

	contract "canonicalingestion"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/hashing"
)

type peerRTTIdentityKV struct{}

func (peerRTTIdentityKV) Proof(context.Context, hashing.Digest) ([]byte, error) {
	return []byte{1}, nil
}
func (peerRTTIdentityKV) Route(_ context.Context, peer, level int) (int, error) {
	return cassandradht.RouteNext(peer, level), nil
}

func TestPeerRTTIsAuditOnlyAndReported(t *testing.T) {
	limits := cassandradht.DefaultLimits()
	native := cassandradht.Config{Peers: 4, Concurrency: limits.Workers, Limits: &limits, Overlay: "native"}
	a := &IncrementalDHT{Options: IncrementalOptions{Cassandra: &native, RunID: "existing", EpochCapacity: 1000}}
	before := a.ConfigHash()
	identity := a.ProofIdentity("stream", "fingerprint")
	native.PeerRTT = contract.DefaultDHTPeerRTT
	if a.ConfigHash() != before || a.ProofIdentity("stream", "fingerprint") != identity {
		t.Fatal("audit RTT changed the persisted dataset binding")
	}
	a.AuditLogicalNodes = 1024
	model := native
	model.Peers = a.AuditLogicalNodes
	client, err := cassandradht.New(context.Background(), peerRTTIdentityKV{}, nil, model)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := a.snapshotProof(client)
	if err != nil || stats.peerRTT != contract.DefaultDHTPeerRTT || stats.logicalNodes != 1024 || stats.routingHops != 10 {
		t.Fatal("audit RTT metadata not propagated", stats, err)
	}
}
