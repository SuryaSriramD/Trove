package auth

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
)

func TestSecretServiceIndependentVectors(t *testing.T) {
	// RFC5869's extract/expand applied independently with Python's HMAC/SHA256:
	// exponent 2, peer public 8, shared integer 64 padded to 128 octets.
	key, err := secretSharedKey(big.NewInt(2), []byte{8})
	if err != nil || hex.EncodeToString(key) != "dd7352d648b81b68e7cfbf88f5c3e2ee" {
		t.Fatal(key, err)
	}
	decode := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// Independent OpenSSL AES-128-CBC/PKCS7 vector (full padding block).
	plaintext := decode("00112233445566778899aabbccddeeff")
	got, err := decryptSecret(decode("000102030405060708090a0b0c0d0e0f"), decode("101112131415161718191a1b1c1d1e1f"), decode("1eca870ffea114b7fd6cf363c30b96b10d47f7c33722bae48a4489a6bb9d993d"))
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatal(got, err)
	}
	for _, peer := range [][]byte{nil, {0}, {1}, secretPrime.Bytes(), make([]byte, 129)} {
		if _, err := secretSharedKey(big.NewInt(2), peer); err == nil {
			t.Fatal("invalid peer accepted")
		}
	}
	if _, err := decryptSecret(key, make([]byte, 15), make([]byte, 16)); err == nil {
		t.Fatal("invalid IV accepted")
	}
	if _, err := decryptSecret(key, make([]byte, 16), make([]byte, 17)); err == nil {
		t.Fatal("partial block accepted")
	}
	if _, err := decryptSecret(key, make([]byte, 16), make([]byte, 16)); err == nil {
		t.Fatal("invalid padding accepted")
	}
}
func TestSecretServiceBoundedRandomizedCiphertext(t *testing.T) {
	key := make([]byte, 16)
	data := bytes.Repeat([]byte("synthetic credential "), 5000)
	iv, a, err := encryptSecret(key, data)
	if err != nil {
		t.Fatal(err)
	}
	other, b, err := encryptSecret(key, data)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(iv, other) || bytes.Equal(a, b) || bytes.Contains(a, []byte("synthetic credential")) {
		t.Fatal("credential transfer not randomized")
	}
	decoded, err := decryptSecret(key, iv, a)
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatal("credential round trip", err)
	}
	for _, invalid := range [][]byte{nil, make([]byte, 131073)} {
		if _, _, err := encryptSecret(key, invalid); err == nil {
			t.Fatal("unbounded credential accepted")
		}
	}
}
