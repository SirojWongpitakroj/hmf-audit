package sig

// Independent tester-agent verification: confirms this package implements
// real secp256k1 ECDSA (not a stub that just compares byte equality) by
// recovering the public key from a signature via go-ethereum's own
// SigToPub/Ecrecover path, independently of Vrfy, and cross-checking it
// against KeyPair.PublicKey. Also stress-tests one-bit-flip rejection
// across every byte position of the signature (not just byte 0), and a
// one-bit flip in the signed digest.

import (
	"testing"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"

	"delia/hash"
)

// A signature must be recoverable to the actual signing key using the
// standard secp256k1 recovery path -- this only works for genuine ECDSA
// signatures over the secp256k1 curve, not a stub/mock.
func TestTesterSignatureRecoversRealSecp256k1Key(t *testing.T) {
	kp, err := Gen()
	if err != nil {
		t.Fatalf("Gen: %v", err)
	}
	digest := hash.H([]byte("recover me"))

	s, err := kp.Sign(digest)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Sign() strips the recovery id byte (documented behavior); re-derive it
	// by trying both possibilities (0 and 1), since Ecrecover needs a 65-byte
	// signature with a recovery id appended.
	var recoveredPub []byte
	for recid := byte(0); recid < 2; recid++ {
		full := append(append([]byte{}, s...), recid)
		pub, err := gethcrypto.Ecrecover(digest[:], full)
		if err != nil {
			continue
		}
		// Ecrecover returns the uncompressed pubkey; compress it to compare
		// against the canonical encoding.
		ecdsaPub, err := gethcrypto.UnmarshalPubkey(pub)
		if err != nil {
			continue
		}
		compressed := gethcrypto.CompressPubkey(ecdsaPub)
		if string(compressed) == string(kp.PublicKey()) {
			recoveredPub = compressed
			break
		}
	}
	if recoveredPub == nil {
		t.Fatalf("could not recover the signing key from the signature via standard secp256k1 ECDSA recovery -- signature does not look like real secp256k1 ECDSA")
	}
}

// A one-bit flip at every byte position of a 64-byte signature must be
// rejected (not just byte 0, which the developer's own test already
// checks). This guards against a broken/partial comparison that only checks
// a prefix.
func TestTesterOneBitFlipAtEveryBytePositionRejected(t *testing.T) {
	kp, _ := Gen()
	digest := hash.H([]byte("stress test"))
	s, err := kp.Sign(digest)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !Vrfy(kp.PublicKey(), digest, s) {
		t.Fatalf("baseline valid signature failed to verify")
	}
	for i := 0; i < len(s); i++ {
		for bit := 0; bit < 8; bit++ {
			tampered := make([]byte, len(s))
			copy(tampered, s)
			tampered[i] ^= 1 << uint(bit)
			if Vrfy(kp.PublicKey(), digest, tampered) {
				t.Fatalf("Vrfy accepted a signature with byte %d bit %d flipped", i, bit)
			}
		}
	}
}

// A one-bit flip in the signed digest (not the signature) must also be
// rejected.
func TestTesterOneBitFlipInDigestRejected(t *testing.T) {
	kp, _ := Gen()
	digest := hash.H([]byte("digest flip test"))
	s, _ := kp.Sign(digest)

	for i := 0; i < len(digest); i++ {
		tampered := digest
		tampered[i] ^= 0x01
		if Vrfy(kp.PublicKey(), tampered, s) {
			t.Fatalf("Vrfy accepted a signature after flipping bit 0 of digest byte %d", i)
		}
	}
}
