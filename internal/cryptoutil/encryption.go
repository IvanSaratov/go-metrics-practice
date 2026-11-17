package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
)

const Encoding = "rsa-aesgcm"

const (
	messageVersion = 1
	aesKeySize     = 32 // 32 байта для AES-256.
)

// Encrypt шифрует данные новым AES ключом, а сам ключ - публичным RSA-ключом.
func Encrypt(publicKey *rsa.PublicKey, plaintext []byte) ([]byte, error) {
	if publicKey == nil || publicKey.N == nil {
		return nil, fmt.Errorf("invalid RSA public key")
	}
	// Для каждого сообщения создаём отдельный случайный AES-ключ.
	sessionKey := make([]byte, aesKeySize)
	if _, err := rand.Read(sessionKey); err != nil {
		return nil, fmt.Errorf("generate AES key: %w", err)
	}
	// RSA защищает AES-ключ, чтобы восстановить его мог только владелец приватного ключа.
	wrappedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, sessionKey, nil)
	if err != nil {
		return nil, fmt.Errorf("encrypt AES key: %w", err)
	}
	gcm, err := newGCM(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}

	// В начало сообщения кладём версию формата и зашифрованный AES-ключ.
	message := append([]byte{messageVersion}, wrappedKey...)
	// Seal добавляет nonce, зашифрованные данные и тег проверки целостности.
	// Версия остаётся открытой, но тоже защищена этим тегом.
	return gcm.Seal(message, nil, plaintext, []byte{messageVersion}), nil
}

// Decrypt восстанавливает AES-ключ и проверяет целостность сообщения при расшифровке.
func Decrypt(privateKey *rsa.PrivateKey, ciphertext []byte) ([]byte, error) {
	if privateKey == nil || privateKey.N == nil {
		return nil, fmt.Errorf("invalid RSA private key")
	}
	// Первый байт — версия, затем идёт RSA-блок размером с ключ в байтах.
	keyEnd := 1 + privateKey.Size()
	if len(ciphertext) < keyEnd {
		return nil, fmt.Errorf("encrypted message is too short")
	}
	if ciphertext[0] != messageVersion {
		return nil, fmt.Errorf("unsupported encrypted message version: %d", ciphertext[0])
	}

	// Приватным RSA-ключом восстанавливаем AES-ключ этого сообщения.
	sessionKey, err := rsa.DecryptOAEP(sha256.New(), nil, privateKey, ciphertext[1:keyEnd], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt AES key: %w", err)
	}
	gcm, err := newGCM(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	// Open расшифровывает данные и проверяет тег. При повреждении вернёт ошибку.
	// Версию передаём отдельно, чтобы проверить и её целостность.
	plaintext, err := gcm.Open(nil, nil, ciphertext[keyEnd:], ciphertext[:1])
	if err != nil {
		return nil, fmt.Errorf("decrypt message: %w", err)
	}
	return plaintext, nil
}

// newGCM создаёт AES-GCM: шифрование с проверкой целостности данных.
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("AES key must be %d bytes", aesKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// Nonce — случайное значение для конкретного шифрования. GCM сам добавляет его
	// в сообщение и читает при расшифровке, поэтому в Seal и Open передаём nil.
	return cipher.NewGCMWithRandomNonce(block)
}
