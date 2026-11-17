// Package crypto holds the four EVSEB primitives: Rand(256) key generation
// (Eq. 3), AES-256 Enc/Dec (Eq. 4, 6, 7, 14, 15, 17), SHA-256 (Eq. 5, 18, 28)
// and the HMAC-SHA256 PRF (Eq. 19). No other primitive is used anywhere.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// KeySize is the 256-bit key length of Eq. 3, in bytes.
const KeySize = 32

// SystemKeys are the four independent keys of Eq. 3.
type SystemKeys struct {
	Kenc    [KeySize]byte // AES key: logs + metadata (Eq. 14, 15)
	Kindex  [KeySize]byte // AES key: tree/BF/inverted index and Policy (Eq. 6, 17)
	Kauth   [KeySize]byte // AES key: user tokens (Eq. 4, 7)
	Ksearch [KeySize]byte // HMAC key: trapdoor PRF (Eq. 18-19)
}

// Rand256 is Rand(256) of Eq. 3: 256 bits from the OS CSPRNG.
func Rand256() ([KeySize]byte, error) {
	var k [KeySize]byte
	if _, err := rand.Read(k[:]); err != nil {
		return k, fmt.Errorf("rand(256): %w", err)
	}
	return k, nil
}

// GenerateKeys is Eq. 3: Kenc, Kindex, Kauth, Ksearch = Rand(256), each drawn
// independently.
func GenerateKeys() (SystemKeys, error) {
	var ks SystemKeys
	var err error
	for _, k := range []*[KeySize]byte{&ks.Kenc, &ks.Kindex, &ks.Kauth, &ks.Ksearch} {
		if *k, err = Rand256(); err != nil {
			return SystemKeys{}, err
		}
	}
	return ks, nil
}

// EncAES encrypts plaintext under a 256-bit key.
//
// Mode is AES-256-CBC with PKCS#7 padding and a fresh random 16-byte IV
// prepended to the output (B16; the paper names no mode - A19).
func EncAES(plaintext []byte, key [KeySize]byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	out := make([]byte, aes.BlockSize+len(padded))
	iv := out[:aes.BlockSize]
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("iv: %w", err)
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[aes.BlockSize:], padded)
	return out, nil
}

// ErrDecrypt is returned for any malformed ciphertext or bad padding.
var ErrDecrypt = errors.New("aes: invalid ciphertext")

// DecAES reverses EncAES.
func DecAES(ciphertext []byte, key [KeySize]byte) ([]byte, error) {
	if len(ciphertext) < 2*aes.BlockSize || len(ciphertext)%aes.BlockSize != 0 {
		return nil, ErrDecrypt
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	iv, body := ciphertext[:aes.BlockSize], ciphertext[aes.BlockSize:]
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, body)
	return pkcs7Unpad(out, aes.BlockSize)
}

// H is SHA-256.
func H(data []byte) [sha256.Size]byte { return sha256.Sum256(data) }

// PRF is Eq. 19: PRF(K, x) = HMAC-SHA256(K, x). Returns 32 bytes.
func PRF(key [KeySize]byte, x []byte) []byte {
	m := hmac.New(sha256.New, key[:])
	m.Write(x)
	return m.Sum(nil)
}

func pkcs7Pad(b []byte, size int) []byte {
	n := size - len(b)%size
	return append(append(make([]byte, 0, len(b)+n), b...), bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(b []byte, size int) ([]byte, error) {
	if len(b) == 0 || len(b)%size != 0 {
		return nil, ErrDecrypt
	}
	n := int(b[len(b)-1])
	if n == 0 || n > size || n > len(b) {
		return nil, ErrDecrypt
	}
	for _, c := range b[len(b)-n:] {
		if int(c) != n {
			return nil, ErrDecrypt
		}
	}
	return b[:len(b)-n], nil
}
