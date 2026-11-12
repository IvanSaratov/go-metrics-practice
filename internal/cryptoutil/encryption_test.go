package cryptoutil

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	largeMessage := make([]byte, 64<<10)
	_, err = rand.Read(largeMessage)
	require.NoError(t, err)

	for _, tt := range []struct {
		name      string
		plaintext []byte
	}{
		{name: "empty"},
		{name: "short", plaintext: []byte(`{"id":"Alloc","type":"gauge","value":42}`)},
		{name: "large", plaintext: largeMessage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := bytes.Clone(tt.plaintext)
			encrypted, err := Encrypt(&key.PublicKey, tt.plaintext)
			require.NoError(t, err)
			require.Equal(t, original, tt.plaintext)
			require.Len(t, encrypted, 1+key.Size()+12+len(tt.plaintext)+16)
			require.Equal(t, byte(1), encrypted[0])

			// Проверяем формат стандартными функциями, независимо от Decrypt.
			keyEnd := 1 + key.Size()
			sessionKey, err := rsa.DecryptOAEP(sha256.New(), nil, key, encrypted[1:keyEnd], nil)
			require.NoError(t, err)
			require.Len(t, sessionKey, 32)
			block, err := aes.NewCipher(sessionKey)
			require.NoError(t, err)
			gcm, err := cipher.NewGCMWithRandomNonce(block)
			require.NoError(t, err)
			plaintext, err := gcm.Open(nil, nil, encrypted[keyEnd:], []byte{1})
			require.NoError(t, err)
			require.Equal(t, tt.plaintext, plaintext)

			originalEncrypted := bytes.Clone(encrypted)
			decrypted, err := Decrypt(key, encrypted)
			require.NoError(t, err)
			require.Equal(t, tt.plaintext, decrypted)
			require.Equal(t, originalEncrypted, encrypted)

			again, err := Encrypt(&key.PublicKey, tt.plaintext)
			require.NoError(t, err)
			require.NotEqual(t, encrypted, again)
			againKey, err := rsa.DecryptOAEP(sha256.New(), nil, key, again[1:keyEnd], nil)
			require.NoError(t, err)
			require.NotEqual(t, sessionKey, againKey)
		})
	}
}

func TestEncryptRejectsInvalidKey(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  *rsa.PublicKey
	}{
		{name: "nil key"},
		{name: "empty key", key: &rsa.PublicKey{}},
		{name: "invalid RSA key", key: &rsa.PublicKey{N: big.NewInt(3), E: 65537}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := Encrypt(tt.key, []byte("metrics"))
			require.Error(t, err)
			require.Nil(t, encrypted)
		})
	}
}

func TestDecryptRejectsInvalidMessage(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encrypted, err := Encrypt(&key.PublicKey, []byte("metrics"))
	require.NoError(t, err)
	keyEnd := 1 + key.Size()

	// AES-128 поддерживается библиотекой, но наш формат требует AES-256.
	shortKey := make([]byte, 16)
	_, err = rand.Read(shortKey)
	require.NoError(t, err)
	wrappedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &key.PublicKey, shortKey, nil)
	require.NoError(t, err)
	block, err := aes.NewCipher(shortKey)
	require.NoError(t, err)
	gcm, err := cipher.NewGCMWithRandomNonce(block)
	require.NoError(t, err)
	shortKeyMessage := append([]byte{1}, wrappedKey...)
	shortKeyMessage = gcm.Seal(shortKeyMessage, nil, []byte("metrics"), []byte{1})

	for _, tt := range []struct {
		name       string
		key        *rsa.PrivateKey
		ciphertext []byte
	}{
		{name: "nil key", ciphertext: encrypted},
		{name: "empty key", key: &rsa.PrivateKey{}, ciphertext: encrypted},
		{name: "wrong key", key: otherKey, ciphertext: encrypted},
		{name: "empty message", key: key},
		{name: "version only", key: key, ciphertext: encrypted[:1]},
		{name: "truncated RSA block", key: key, ciphertext: encrypted[:keyEnd-1]},
		{name: "missing GCM data", key: key, ciphertext: encrypted[:keyEnd]},
		{name: "truncated nonce", key: key, ciphertext: encrypted[:keyEnd+11]},
		{name: "truncated tag", key: key, ciphertext: encrypted[:len(encrypted)-1]},
		{name: "unknown version", key: key, ciphertext: corruptByte(encrypted, 0)},
		{name: "corrupted RSA block", key: key, ciphertext: corruptByte(encrypted, 1)},
		{name: "corrupted nonce", key: key, ciphertext: corruptByte(encrypted, keyEnd)},
		{name: "corrupted ciphertext", key: key, ciphertext: corruptByte(encrypted, keyEnd+12)},
		{name: "corrupted tag", key: key, ciphertext: corruptByte(encrypted, len(encrypted)-1)},
		{name: "trailing data", key: key, ciphertext: append(bytes.Clone(encrypted), 0)},
		{name: "wrong AES key size", key: key, ciphertext: shortKeyMessage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := bytes.Clone(tt.ciphertext)
			decrypted, err := Decrypt(tt.key, tt.ciphertext)
			require.Error(t, err)
			require.Nil(t, decrypted)
			require.Equal(t, original, tt.ciphertext)
		})
	}
}

func corruptByte(data []byte, index int) []byte {
	corrupted := bytes.Clone(data)
	corrupted[index] ^= 0xff
	return corrupted
}
