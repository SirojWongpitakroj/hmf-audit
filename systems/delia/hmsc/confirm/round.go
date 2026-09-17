package confirm

import "errors"

// RoundConfig is the per-instance round configuration ARCH-009/010 requires:
// "beta is not fixed in different state channel instances... every state
// channel instance could have different value of beta." The protocol gives
// no default (GAP-55/56/67 ruling): a zero/unset beta is refused, not
// silently defaulted. A round is conceptually one signed round state
// followed by up to beta no-signed round states (ARCH-008); the on-chain
// enforcement of round boundaries itself is ARCH-008, deferred past this
// milestone — RoundConfig only carries the required configuration value.
type RoundConfig struct {
	Beta uint32
}

// NewRoundConfig validates beta (ARCH-010: required, no protocol default).
func NewRoundConfig(beta uint32) (RoundConfig, error) {
	if beta == 0 {
		return RoundConfig{}, errors.New("confirm: beta is required per-instance configuration (ARCH-010), no default")
	}
	return RoundConfig{Beta: beta}, nil
}
