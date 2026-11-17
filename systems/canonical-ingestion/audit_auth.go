package canonicalingestion

import "context"

type authHashCountKey struct{}

// WithAuthHashCount marks a measurement pass that counts the hash invocations
// of verification. Such a pass runs independent targets one at a time, because
// the counter is process-wide and a scheme's construction and verification
// would otherwise overlap in time. It changes scheduling only, never what is
// verified, and is used by the authentication-cost experiment alone.
func WithAuthHashCount(ctx context.Context) context.Context {
	return context.WithValue(ctx, authHashCountKey{}, true)
}

// AuthHashCountFrom reports whether the pass counts verification hashes.
func AuthHashCountFrom(ctx context.Context) bool {
	v, _ := ctx.Value(authHashCountKey{}).(bool)
	return v
}
