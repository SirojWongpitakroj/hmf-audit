package bhash

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBlake2b256EmptyVector checks against the well-known RFC 7693 /
// reference BLAKE2b-256 digest of the empty input, to pin down that H is
// exactly unkeyed BLAKE2b-256 and nothing else (A2, R-CRYPTO-001).
func TestBlake2b256EmptyVector(t *testing.T) {
	want := "0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8"
	got := H(nil)
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("H(empty) = %x, want %s", got, want)
	}
}

// TestDigestWidth ensures the digest is always exactly 32 bytes
// (R-CRYPTO-014 — no truncation below 256 bits).
func TestDigestWidth(t *testing.T) {
	d := H([]byte("anything"))
	if len(d) != DigestSize {
		t.Fatalf("digest width = %d, want %d", len(d), DigestSize)
	}
}

func TestUint64BEFixedWidth(t *testing.T) {
	b := Uint64BE(1)
	if len(b) != 8 {
		t.Fatalf("Uint64BE width = %d, want 8", len(b))
	}
	if !bytes.Equal(b, []byte{0, 0, 0, 0, 0, 0, 0, 1}) {
		t.Fatalf("Uint64BE(1) = %x, want big-endian 1", b)
	}
}

func TestInt64BENegative(t *testing.T) {
	b := Int64BE(-1)
	if !bytes.Equal(b, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("Int64BE(-1) = %x, want all-0xff", b)
	}
}

// TestFramedBytesInjective checks that two different (length, content)
// combinations never collide in their framed byte representation, which is
// exactly the property R-SEC-013 requires from the encoding.
func TestFramedBytesInjective(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("a"),
		[]byte("aa"),
		[]byte("ab"),
		[]byte("b"),
	}
	seen := map[string]string{}
	for _, c := range cases {
		framed := string(FramedBytes(c))
		if prev, ok := seen[framed]; ok {
			t.Fatalf("collision: %q and %q both frame to the same bytes", prev, c)
		}
		seen[framed] = string(c)
	}
}

// TestHConcatOrderMatters ensures the concatenation order changes the hash,
// which is what backs R-CRYPTO-009's left-to-right child order requirement.
func TestHConcatOrderMatters(t *testing.T) {
	a := FramedString("Error")
	b := FramedString("PUT")
	h1 := HashConcat(a, b)
	h2 := HashConcat(b, a)
	if h1 == h2 {
		t.Fatalf("HashConcat(a,b) == HashConcat(b,a); order must matter")
	}
}

// TestMapIterationIndependence builds the same hash input from data held in
// a Go map, but always via an explicit ordered slice, 100 times, and checks
// the digest never varies — the primitive-level analogue of T-057.
func TestMapIterationIndependence(t *testing.T) {
	data := map[string][]byte{
		"c": FramedString("c"),
		"a": FramedString("a"),
		"b": FramedString("b"),
	}
	order := []string{"a", "b", "c"}
	first := computeFromMap(data, order)
	for i := 0; i < 100; i++ {
		got := computeFromMap(data, order)
		if got != first {
			t.Fatalf("iteration %d: digest changed: got %x want %x", i, got, first)
		}
	}
}

func computeFromMap(data map[string][]byte, order []string) Digest {
	parts := make([][]byte, 0, len(order))
	for _, k := range order {
		parts = append(parts, data[k])
	}
	return HashConcat(parts...)
}

func TestDigestBytesRoundTrip(t *testing.T) {
	d := H([]byte("x"))
	b := DigestBytes(d)
	if len(b) != DigestSize {
		t.Fatalf("DigestBytes length = %d, want %d", len(b), DigestSize)
	}
	if !bytes.Equal(b, d[:]) {
		t.Fatalf("DigestBytes(d) != d[:]")
	}
}

// TestCrossProcessDeterminism: T-056 — the SAME logical hash computation,
// run in the current process and in a genuinely separate OS process,
// produces byte-identical output. This spawns the test binary itself as a
// subprocess (the standard Go "helper process" pattern) rather than
// asserting anything about in-process state, so it is a real two-process
// check, not a simulation.
func TestCrossProcessDeterminism(t *testing.T) {
	inProcess := H([]byte("cross-process-determinism-fixture"))

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessHash", "-test.v")
	cmd.Env = append(os.Environ(), "BLAS_HELPER_PROCESS=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper subprocess failed: %v", err)
	}
	// Extract the hex digest the helper printed on its own marker line.
	var subprocessHex string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "HELPER_HASH=") {
			subprocessHex = strings.TrimPrefix(line, "HELPER_HASH=")
		}
	}
	if subprocessHex == "" {
		t.Fatalf("did not find HELPER_HASH marker in subprocess output:\n%s", out)
	}
	want := hex.EncodeToString(inProcess[:])
	if subprocessHex != want {
		t.Fatalf("subprocess hash = %s, want %s (in-process)", subprocessHex, want)
	}
}

// TestHelperProcessHash is not a real test: it is invoked as a subprocess
// by TestCrossProcessDeterminism (guarded by BLAS_HELPER_PROCESS so it is a
// silent no-op under a normal `go test` run).
func TestHelperProcessHash(t *testing.T) {
	if os.Getenv("BLAS_HELPER_PROCESS") != "1" {
		return
	}
	d := H([]byte("cross-process-determinism-fixture"))
	fmt.Println("HELPER_HASH=" + hex.EncodeToString(d[:]))
}

// TestCrossArchBuildsCleanly: T-056 (GOARCH dimension) — the module
// cross-compiles cleanly for architectures other than the host (arm64,
// 386), which is a necessary precondition for cross-arch determinism and
// is as far as this can be verified without an emulator/second machine to
// actually EXECUTE the resulting binary (recorded as a scope limitation,
// not silently claimed as a full cross-arch run). Because this codebase
// has no cgo, no unsafe pointer arithmetic, and no arch-conditional code
// (grep-audited in the `audit` package), a successful cross-compile is
// strong evidence — not a full proof — that the hash output would also
// match if it could be executed.
func TestCrossArchBuildsCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cross-arch build check in -short mode")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, goarch := range []string{"arm64", "386"} {
		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOARCH="+goarch, "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cross-compiling for GOARCH=%s failed: %v\n%s", goarch, err, out)
		}
	}
}

func TestInvocationCounterCountsOneHashPerProtocolInput(t *testing.T) {
	H([]byte("not counted: counting is off by default"))
	StartCount()
	H([]byte("a"))
	HashConcat([]byte("b"), []byte("c"), []byte("d")) // one invocation over one concatenated input
	H(nil)
	if got := StopCount(); got != 3 {
		t.Fatalf("counted %d invocations, want 3", got)
	}
	H([]byte("after stop"))
	StartCount()
	if got := StopCount(); got != 0 {
		t.Fatalf("counter not reset by StartCount: %d", got)
	}
}
