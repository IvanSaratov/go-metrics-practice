package helpers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{
			name:    "adds http scheme to host address",
			address: "localhost:8080",
			want:    "http://localhost:8080",
		},
		{
			name:    "keeps http URL unchanged",
			address: "http://localhost:8080",
			want:    "http://localhost:8080",
		},
		{
			name:    "keeps https URL unchanged",
			address: "https://example.com",
			want:    "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeBaseURL(tt.address)

			require.Equal(t, tt.want, got)
		})
	}
}
