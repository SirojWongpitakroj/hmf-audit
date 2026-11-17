package canonicalingestion

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const ColdApplicationCachePolicy = "Cold application-level audit cache, naturally warm database/storage infrastructure."

type auditCacheResetKey struct{}

// WithAuditCacheReset registers all request-independent caches owned by the
// adapter. The hook must return only after verifying they are empty. A no-op is
// appropriate only for an adapter with exclusively request-local audit caches.
func WithAuditCacheReset(ctx context.Context, reset func(context.Context) error) context.Context {
	return context.WithValue(ctx, auditCacheResetKey{}, reset)
}

// AuditWithColdCaches resets outside the adapter's timed request, including on
// error. It never touches database, driver connection or filesystem caches.
// Callers must hold their dataset lease and serialize independent audit runs.
func AuditWithColdCaches(ctx context.Context, run func(context.Context) (AuditMetrics, error)) (m AuditMetrics, err error) {
	reset, ok := ctx.Value(auditCacheResetKey{}).(func(context.Context) error)
	if !ok || reset == nil || run == nil {
		return m, errors.New("audit requires an explicit application-cache reset hook and measurement")
	}
	resetBefore := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		m.ApplicationCacheResetBefore = resetBefore
		if e := reset(cleanup); e != nil {
			err = errors.Join(err, fmt.Errorf("audit cache cleanup: %w", e))
		} else {
			m.ApplicationCacheResetAfter = true
		}
	}()
	if err = reset(ctx); err != nil {
		return m, fmt.Errorf("audit cache reset: %w", err)
	}
	resetBefore = true
	return run(ctx)
}
