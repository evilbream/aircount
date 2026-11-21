package tplink

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
)

const signChunk = 53 // 512-bit signing key: 64 bytes - 11 bytes PKCS#1 v1.5 overhead

func randomHex16() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type aesCBC struct{ key, iv string }

func (a aesCBC) encrypt(plain []byte) (string, error) {
	block, err := aes.NewCipher([]byte(a.key))
	if err != nil {
		return "", err
	}
	n := aes.BlockSize - len(plain)%aes.BlockSize
	buf := append(slices.Clone(plain), bytes.Repeat([]byte{byte(n)}, n)...)
	cipher.NewCBCEncrypter(block, []byte(a.iv)).CryptBlocks(buf, buf)
	return base64.StdEncoding.EncodeToString(buf), nil
}

func (a aesCBC) decrypt(b64 string) ([]byte, error) {
	buf, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(buf) == 0 || len(buf)%aes.BlockSize != 0 {
		return nil, errors.New("tplink: bad ciphertext length")
	}
	block, err := aes.NewCipher([]byte(a.key))
	if err != nil {
		return nil, err
	}
	cipher.NewCBCDecrypter(block, []byte(a.iv)).CryptBlocks(buf, buf)
	n := int(buf[len(buf)-1])
	if n == 0 || n > aes.BlockSize {
		return nil, errors.New("tplink: bad padding")
	}
	return buf[:len(buf)-n], nil
}

type rsaPub struct {
	n    *big.Int
	e    *big.Int
	size int // modulus length in bytes
}

func parseRSA(nHex, eHex string) (rsaPub, error) {
	n, ok1 := new(big.Int).SetString(nHex, 16)
	e, ok2 := new(big.Int).SetString(eHex, 16)
	if !ok1 || !ok2 {
		return rsaPub{}, fmt.Errorf("tplink: bad rsa key %q/%q", nHex, eHex)
	}
	return rsaPub{n: n, e: e, size: (n.BitLen() + 7) / 8}, nil
}

// encryptHex is RSAES-PKCS1-v1_5 by hand: crypto/rsa rejects keys < 1024 bit
// since Go 1.24 (GODEBUG rsa1024min), and the router signing key is 512 bit.
func (k rsaPub) encryptHex(msg []byte) (string, error) {
	if len(msg) > k.size-11 {
		return "", errors.New("tplink: rsa message too long")
	}
	em := make([]byte, k.size) // 0x00 0x02 PS(non-zero) 0x00 msg
	em[1] = 0x02
	ps := em[2 : k.size-len(msg)-1]
	rand.Read(ps)
	for i := range ps {
		for ps[i] == 0 {
			rand.Read(ps[i : i+1])
		}
	}
	copy(em[k.size-len(msg):], msg)
	c := new(big.Int).Exp(new(big.Int).SetBytes(em), k.e, k.n)
	return hex.EncodeToString(c.FillBytes(make([]byte, k.size))), nil
}

// sign builds "[k=..&i=..&]h=..&s=.." and RSA-encrypts it in 53-byte chunks.
func (c *Client) sign(dataLen int, login bool) (string, error) {
	s := fmt.Sprintf("h=%s&s=%d", c.hash, c.seq+dataLen)
	if login {
		s = fmt.Sprintf("k=%s&i=%s&", c.aes.key, c.aes.iv) + s
	}
	var out strings.Builder
	for i := 0; i < len(s); i += signChunk {
		part, err := c.signKey.encryptHex([]byte(s[i:min(i+signChunk, len(s))]))
		if err != nil {
			return "", err
		}
		out.WriteString(part)
	}
	return out.String(), nil
}
