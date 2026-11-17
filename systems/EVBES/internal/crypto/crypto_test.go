package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestGenerateKeysDistinctAndFullLength(t *testing.T) {
	ks, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys := [][KeySize]byte{ks.Kenc, ks.Kindex, ks.Kauth, ks.Ksearch}
	var zero [KeySize]byte
	for i := range keys {
		if keys[i] == zero {
			t.Fatalf("key %d is all zero", i)
		}
		for j := i + 1; j < len(keys); j++ {
			if keys[i] == keys[j] {
				t.Fatalf("keys %d and %d identical", i, j)
			}
		}
	}
	ks2, _ := GenerateKeys()
	if ks2.Kenc == ks.Kenc {
		t.Fatal("two GenerateKeys calls produced the same Kenc")
	}
}

func TestAESRoundTripAllLengths(t *testing.T) {
	key, _ := Rand256()
	for n := 0; n <= 70; n++ {
		pt := bytes.Repeat([]byte{byte(n)}, n)
		ct, err := EncAES(pt, key)
		if err != nil {
			t.Fatal(err)
		}
		if len(ct)%16 != 0 || len(ct) < 32 {
			t.Fatalf("len %d: bad ciphertext length %d", n, len(ct))
		}
		got, err := DecAES(ct, key)
		if err != nil || !bytes.Equal(got, pt) {
			t.Fatalf("len %d: round trip failed: %v", n, err)
		}
	}
}

func TestAESRandomIVAndWrongKey(t *testing.T) {
	k1, _ := Rand256()
	k2, _ := Rand256()
	pt := []byte("firewall deny tcp 10.0.0.1")
	a, _ := EncAES(pt, k1)
	b, _ := EncAES(pt, k1)
	if bytes.Equal(a, b) {
		t.Fatal("same plaintext encrypted twice gave identical ciphertext (IV not random)")
	}
	if got, err := DecAES(a, k2); err == nil && bytes.Equal(got, pt) {
		t.Fatal("decryption under the wrong key recovered plaintext")
	}
	if _, err := DecAES(a[:20], k1); err == nil {
		t.Fatal("truncated ciphertext accepted")
	}
}

// FIPS-197 Appendix C.3 (AES-256) vector. With IV = 0, CBC's first block is
// raw AES, so DecAES(0^16 || C || E(K, C xor 0x10^16)) must return P. This
// pins EncAES/DecAES to AES with a 256-bit key.
func TestAES256KnownAnswer(t *testing.T) {
	var key [KeySize]byte
	kb, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	copy(key[:], kb)
	pt, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	c, _ := hex.DecodeString("8ea2b7ca516745bfeafc49904b496089")
	blk, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	pad := make([]byte, 16)
	for i := range pad {
		pad[i] = c[i] ^ 0x10
	}
	blk.Encrypt(pad, pad)
	ct := append(append(make([]byte, 16), c...), pad...)
	got, err := DecAES(ct, key)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatalf("AES-256 known answer failed: %x %v", got, err)
	}
}

func TestHIsSHA256(t *testing.T) {
	got := H([]byte("abc"))
	want, _ := hex.DecodeString("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	if !bytes.Equal(got[:], want) {
		t.Fatalf("H(abc) = %x", got)
	}
}

func TestPRFIsHMACSHA256(t *testing.T) {
	k, _ := Rand256()
	x := []byte("x")
	m := hmac.New(sha256.New, k[:])
	m.Write(x)
	if !bytes.Equal(PRF(k, x), m.Sum(nil)) {
		t.Fatal("PRF != HMAC-SHA256")
	}
	if len(PRF(k, x)) != 32 {
		t.Fatal("PRF output not 32 bytes")
	}
}
