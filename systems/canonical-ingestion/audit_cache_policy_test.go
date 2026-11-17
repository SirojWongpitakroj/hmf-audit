package canonicalingestion

import (
	"context"
	"errors"
	"testing"
)

func TestColdApplicationCachesEveryRunAndFailure(t *testing.T) {
	for _, scheme := range []string{"MVA-PHV", "BLAS", "DELIA", "DHT-backed"} {
		t.Run(scheme, func(t *testing.T) {
			cache := map[int]bool{1: true}
			resets := 0
			calls := 0
			ctx := WithAuditCacheReset(context.Background(), func(context.Context) error { clear(cache); resets++; return nil })
			for run := range 35 { // 5 warmups and 30 measured requests.
				_, err := AuditWithColdCaches(ctx, func(context.Context) (AuditMetrics, error) {
					if len(cache) != 0 {
						t.Fatal("cross-run cache reuse", run)
					}
					cache[1] = true
					calls++
					if !cache[1] {
						t.Fatal("request-local reuse disabled")
					}
					return AuditMetrics{}, nil
				})
				if err != nil || len(cache) != 0 {
					t.Fatal("cache not cleared", err)
				}
			}
			if calls != 35 || resets != 70 {
				t.Fatal("wrong repetition/reset count", calls, resets)
			}
			failure := errors.New("injected verification failure")
			_, err := AuditWithColdCaches(ctx, func(context.Context) (AuditMetrics, error) { cache[1] = true; return AuditMetrics{}, failure })
			if !errors.Is(err, failure) || len(cache) != 0 {
				t.Fatal("failure bypassed cleanup", err)
			}
		})
	}
}

func TestColdAuditRejectsMissingResetAndCleansCancelledRun(t *testing.T) {
	if _, err := AuditWithColdCaches(context.Background(), nil); err == nil {
		t.Fatal("missing reset accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanups := 0
	ctx = WithAuditCacheReset(ctx, func(c context.Context) error {
		if c.Err() != nil {
			t.Fatal("cleanup inherited cancellation")
		}
		cleanups++
		return nil
	})
	_, err := AuditWithColdCaches(ctx, func(context.Context) (AuditMetrics, error) { cancel(); return AuditMetrics{}, context.Canceled })
	if !errors.Is(err, context.Canceled) || cleanups != 2 {
		t.Fatal(err, cleanups)
	}
}
