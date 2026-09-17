package confirm

import "testing"

// ARCH-009/010: beta is required per-instance configuration; no protocol
// default.
func TestNewRoundConfigRejectsZero(t *testing.T) {
	if _, err := NewRoundConfig(0); err == nil {
		t.Fatal("expected an error for beta = 0 (no default)")
	}
}

func TestNewRoundConfigAcceptsPositive(t *testing.T) {
	cfg, err := NewRoundConfig(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Beta != 5 {
		t.Fatalf("got beta %d, want 5", cfg.Beta)
	}
}

// Two instances can have different beta values (ARCH-009).
func TestRoundConfigPerInstance(t *testing.T) {
	a, err := NewRoundConfig(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := NewRoundConfig(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Beta == b.Beta {
		t.Fatal("expected independently configurable beta values")
	}
}
