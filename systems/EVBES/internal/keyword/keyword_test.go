package keyword

import (
	"slices"
	"testing"
)

func TestPreprocess(t *testing.T) {
	got := Preprocess("DENY tcp from 10.0.0.1:443 to the Gateway, [ALERT]!")
	want := "deny tcp 10 0 0 1 443 gateway alert"
	if got != want {
		t.Fatalf("Preprocess = %q, want %q", got, want)
	}
}

func TestTokenizeIsSet(t *testing.T) {
	got := Tokenize("deny tcp deny udp tcp")
	if !slices.Equal(got, []string{"deny", "tcp", "udp"}) {
		t.Fatalf("Tokenize = %v", got)
	}
}

func TestSelectB15(t *testing.T) {
	got := Select([]string{"deny", "tc", "443", "gw1", "alert"}, DefaultMinLength)
	if !slices.Equal(got, []string{"deny", "alert"}) {
		t.Fatalf("Select = %v", got)
	}
	if got := Select([]string{"deny", "alert"}, 5); !slices.Equal(got, []string{"alert"}) {
		t.Fatalf("configurable threshold ignored: %v", got)
	}
}

func TestExtract(t *testing.T) {
	got := Extract("Firewall: DENY inbound TCP connection from 10.1.2.3 to port 22 - policy BLOCK")
	want := []string{"firewall", "deny", "inbound", "tcp", "connection", "port", "policy", "block"}
	if !slices.Equal(got, want) {
		t.Fatalf("Extract = %v", got)
	}
}
