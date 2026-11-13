package cryptoutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// LoadPublicKey читает публичный ключ из PEM файла
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	if path == "" {
		return nil, nil
	}
	block, err := readKeyPEM(path)
	if err != nil {
		return nil, err
	}

	var parsed any
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		parsed, err = x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported public key PEM type: %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key must be RSA")
	}
	// Пробное шифрование проверяет параметры RSA и место для нашего AES-ключа.
	if _, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, make([]byte, aesKeySize), nil); err != nil {
		return nil, fmt.Errorf("validate RSA public key: %w", err)
	}
	return key, nil
}

// LoadPrivateKey читает приватный ключ из PEM файла
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return nil, nil
	}
	block, err := readKeyPEM(path)
	if err != nil {
		return nil, err
	}

	var parsed any
	switch block.Type {
	case "PRIVATE KEY":
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		parsed, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key PEM type: %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key must be RSA")
	}
	return key, nil
}

func readKeyPEM(path string) (*pem.Block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file %q: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("decode key file %q: no PEM block found", path)
	}
	return block, nil
}
