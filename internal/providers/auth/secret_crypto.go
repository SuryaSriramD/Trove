package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math/big"
)

// Secret Service specifies the second Oakley group and HKDF-SHA256. This
// compatibility transport is only used on the authenticated local session bus.
// It is never selected for internet transport or replaced by the plain algorithm.
var secretPrime = func() *big.Int {
	p, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF", 16)
	return p
}()

func secretKeypair() (*big.Int, []byte, error) {
	// A uniformly random 256-bit exponent is sufficient for this fixed group.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, nil, err
	}
	defer clear(b)
	x := new(big.Int).SetBytes(b)
	if x.Cmp(big.NewInt(2)) < 0 {
		return secretKeypair()
	}
	y := new(big.Int).Exp(big.NewInt(2), x, secretPrime)
	return x, y.Bytes(), nil
}
func secretSharedKey(private *big.Int, peer []byte) ([]byte, error) {
	if len(peer) == 0 || len(peer) > 128 {
		return nil, fmt.Errorf("invalid Secret Service public key")
	}
	y := new(big.Int).SetBytes(peer)
	upper := new(big.Int).Sub(secretPrime, big.NewInt(2))
	if y.Cmp(big.NewInt(2)) < 0 || y.Cmp(upper) > 0 {
		return nil, fmt.Errorf("invalid Secret Service public key")
	}
	q := new(big.Int).Rsh(new(big.Int).Sub(secretPrime, big.NewInt(1)), 1)
	if new(big.Int).Exp(y, q, secretPrime).Cmp(big.NewInt(1)) != 0 {
		return nil, fmt.Errorf("invalid Secret Service subgroup")
	}
	shared := make([]byte, 128)
	new(big.Int).Exp(y, private, secretPrime).FillBytes(shared)
	defer clear(shared)
	return hkdf.Key(sha256.New, shared, nil, "", 16)
}
func encryptSecret(key, data []byte) (iv, encrypted []byte, err error) {
	if len(data) == 0 || len(data) > 131072 {
		return nil, nil, fmt.Errorf("invalid credential record")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	iv = make([]byte, aes.BlockSize)
	if _, err = rand.Read(iv); err != nil {
		return nil, nil, err
	}
	n := aes.BlockSize - len(data)%aes.BlockSize
	plain := make([]byte, len(data)+n)
	copy(plain, data)
	for i := len(data); i < len(plain); i++ {
		plain[i] = byte(n)
	}
	defer clear(plain)
	encrypted = make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)
	return iv, encrypted, nil
}
func decryptSecret(key, iv, encrypted []byte) ([]byte, error) {
	if len(iv) != aes.BlockSize || len(encrypted) == 0 || len(encrypted) > 131072+aes.BlockSize || len(encrypted)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid encrypted credential")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, encrypted)
	n := int(plain[len(plain)-1])
	valid := subtle.ConstantTimeLessOrEq(1, n) & subtle.ConstantTimeLessOrEq(n, aes.BlockSize)
	for i := 1; i <= aes.BlockSize; i++ {
		inside := subtle.ConstantTimeLessOrEq(i, n)
		equal := subtle.ConstantTimeByteEq(plain[len(plain)-i], byte(n))
		valid &= subtle.ConstantTimeSelect(inside, equal, 1)
	}
	if valid != 1 || len(plain)-n == 0 || len(plain)-n > 131072 {
		clear(plain)
		return nil, fmt.Errorf("invalid encrypted credential padding")
	}
	out := append([]byte(nil), plain[:len(plain)-n]...)
	clear(plain)
	return out, nil
}
