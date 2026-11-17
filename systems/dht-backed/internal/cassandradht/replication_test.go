package cassandradht

import "testing"

func TestReplicationFactorSumsDataCenters(t *testing.T) {
	for _, c := range []struct {
		in   map[string]string
		want int
	}{
		{map[string]string{"class": "org.apache.cassandra.locator.NetworkTopologyStrategy", "dc1": "3"}, 3},
		{map[string]string{"class": "org.apache.cassandra.locator.NetworkTopologyStrategy", "dc1": "1"}, 1},
		{map[string]string{"class": "org.apache.cassandra.locator.SimpleStrategy", "replication_factor": "1"}, 1},
		{map[string]string{"class": "NetworkTopologyStrategy", "a": "2", "b": "1"}, 3},
	} {
		if got, err := replicationFactor(c.in); err != nil || got != c.want {
			t.Fatal(c.in, got, err)
		}
	}
	for _, bad := range []map[string]string{{"class": "x"}, {"dc1": "x"}, {"dc1": "-1"}, {"dc1": "0"}} {
		if _, err := replicationFactor(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}
