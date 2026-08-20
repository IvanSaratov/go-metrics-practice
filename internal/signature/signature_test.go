package signature

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSumReturnsHMACSHA256Hex(t *testing.T) {
	const want = "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"

	got := Sum([]byte("The quick brown fox jumps over the lazy dog"), "key")

	require.Equal(t, want, got)
}

func TestVerifyAcceptsOnlyMatchingSignature(t *testing.T) {
	const (
		body = "request body"
		key  = "secret"
		hash = "284ecbd7ee5e3868010384e98f2f397a4d733937d14b0a19dcff5ae95739962b"
	)

	tests := []struct {
		name string
		body string
		key  string
		hash string
		want bool
	}{
		{
			name: "matching signature",
			body: body,
			key:  key,
			hash: hash,
			want: true,
		},
		{
			name: "changed body",
			body: body + "!",
			key:  key,
			hash: hash,
		},
		{
			name: "different key",
			body: body,
			key:  key + "!",
			hash: hash,
		},
		{
			name: "malformed hex",
			body: body,
			key:  key,
			hash: "not-hex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify([]byte(tt.body), tt.key, tt.hash))
		})
	}
}
