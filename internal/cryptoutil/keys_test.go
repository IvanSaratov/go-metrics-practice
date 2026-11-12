package cryptoutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadPublicKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key := &privateKey.PublicKey
	pkix, err := x509.MarshalPKIXPublicKey(key)
	require.NoError(t, err)
	otherKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherPKIX, err := x509.MarshalPKIXPublicKey(otherKey)
	require.NoError(t, err)

	tests := []struct {
		name    string
		path    string
		want    *rsa.PublicKey
		wantErr bool
	}{
		{name: "empty path"},
		{name: "PKIX", path: writeKeyFile(t, keyPEM("PUBLIC KEY", pkix)), want: key},
		{name: "PKCS1", path: writeKeyFile(t, keyPEM("RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(key))), want: key},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing.pem"), wantErr: true},
		{name: "empty file", path: writeKeyFile(t, nil), wantErr: true},
		{name: "invalid PEM", path: writeKeyFile(t, []byte("not PEM")), wantErr: true},
		{name: "invalid PKIX", path: writeKeyFile(t, keyPEM("PUBLIC KEY", []byte("invalid DER"))), wantErr: true},
		{name: "invalid PKCS1", path: writeKeyFile(t, keyPEM("RSA PUBLIC KEY", []byte("invalid DER"))), wantErr: true},
		{name: "not RSA", path: writeKeyFile(t, keyPEM("PUBLIC KEY", otherPKIX)), wantErr: true},
		{name: "unsupported PEM type", path: writeKeyFile(t, keyPEM("CERTIFICATE", pkix)), wantErr: true},
		{name: "private key", path: writeKeyFile(t, keyPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(privateKey))), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadPublicKey(tt.path)

			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.True(t, tt.want.Equal(got))
		})
	}
}

func TestLoadPrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherPKCS8, err := x509.MarshalPKCS8PrivateKey(otherKey)
	require.NoError(t, err)

	// Сохраняем корректный ASN.1, но делаем параметры RSA неверными.
	invalidKey := *key
	invalidKey.D = big.NewInt(1)

	tests := []struct {
		name    string
		path    string
		want    *rsa.PrivateKey
		wantErr bool
	}{
		{name: "empty path"},
		{name: "PKCS8", path: writeKeyFile(t, keyPEM("PRIVATE KEY", pkcs8)), want: key},
		{name: "PKCS1", path: writeKeyFile(t, keyPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))), want: key},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing.pem"), wantErr: true},
		{name: "empty file", path: writeKeyFile(t, nil), wantErr: true},
		{name: "invalid PEM", path: writeKeyFile(t, []byte("not PEM")), wantErr: true},
		{name: "invalid PKCS8", path: writeKeyFile(t, keyPEM("PRIVATE KEY", []byte("invalid DER"))), wantErr: true},
		{name: "invalid PKCS1", path: writeKeyFile(t, keyPEM("RSA PRIVATE KEY", []byte("invalid DER"))), wantErr: true},
		{name: "not RSA", path: writeKeyFile(t, keyPEM("PRIVATE KEY", otherPKCS8)), wantErr: true},
		{name: "unsupported PEM type", path: writeKeyFile(t, keyPEM("CERTIFICATE", pkcs8)), wantErr: true},
		{name: "public key", path: writeKeyFile(t, keyPEM("RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(&key.PublicKey))), wantErr: true},
		{name: "encrypted PEM type", path: writeKeyFile(t, keyPEM("ENCRYPTED PRIVATE KEY", pkcs8)), wantErr: true},
		{name: "invalid RSA parameters", path: writeKeyFile(t, keyPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(&invalidKey))), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadPrivateKey(tt.path)

			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.True(t, tt.want.Equal(got))
		})
	}
}

func keyPEM(blockType string, data []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data})
}

func writeKeyFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key.pem")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}
