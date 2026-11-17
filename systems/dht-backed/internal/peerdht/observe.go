package peerdht

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	contract "canonicalingestion"
)

// Observe runs outside the timed native audit. Each actual peer process is
// reported separately; four bounded caches are not represented as one cache.
func (c *Client) Observe(ctx context.Context, metrics *contract.AuditMetrics) ([]contract.ExternalProcess, error) {
	var out []contract.ExternalProcess
	metrics.CacheBytes = 0
	metrics.CacheEntries = 0
	for i, endpoint := range c.Config.Peers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/stats", nil)
		if err != nil {
			return nil, err
		}
		response, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, int64(c.Config.Limits.MaxRequestBytes)+1))
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK || len(raw) > c.Config.Limits.MaxRequestBytes {
			return nil, fmt.Errorf("peer %d statistics unavailable", i)
		}
		var stats Stats
		if err = json.Unmarshal(raw, &stats); err != nil {
			return nil, err
		}
		l := c.Config.Limits
		if stats.CacheBytes > uint64(l.CacheBytes) || stats.CacheEntries > uint64(l.CacheEntries) {
			return nil, fmt.Errorf("peer %d exceeded cache budget", i)
		}
		metrics.CacheBytes += stats.CacheBytes
		metrics.CacheEntries += stats.CacheEntries
		out = append(out, contract.ExternalProcess{ID: fmt.Sprint(i), Role: "DHT peer", Memory: stats.Memory, CacheEnabled: stats.CacheEnabled, CacheBytes: stats.CacheBytes, CacheEntries: stats.CacheEntries, CacheMaxBytes: uint64(l.CacheBytes), CacheMaxEntries: uint64(l.CacheEntries)})
	}
	return out, nil
}
