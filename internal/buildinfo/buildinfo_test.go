package buildinfo_test

import (
	"bytes"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/buildinfo"
	"github.com/stretchr/testify/require"
)

func TestPrint(t *testing.T) {
	tests := []struct {
		name    string
		version string
		date    string
		commit  string
		want    string
	}{
		{
			name: "empty values",
			want: "Build version: N/A\nBuild date: N/A\nBuild commit: N/A\n",
		},
		{
			name: "all values", version: "v1.2.3", date: "2026-11-01 12:34:56", commit: "abc1234",
			want: "Build version: v1.2.3\nBuild date: 2026-11-01 12:34:56\nBuild commit: abc1234\n",
		},
		{
			name: "empty version", date: "2026-11-01", commit: "abc1234",
			want: "Build version: N/A\nBuild date: 2026-11-01\nBuild commit: abc1234\n",
		},
		{
			name: "empty date", version: "v1.2.3", commit: "abc1234",
			want: "Build version: v1.2.3\nBuild date: N/A\nBuild commit: abc1234\n",
		},
		{
			name: "empty commit", version: "v1.2.3", date: "2026-11-01",
			want: "Build version: v1.2.3\nBuild date: 2026-11-01\nBuild commit: N/A\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			buildinfo.Print(&output, tt.version, tt.date, tt.commit)
			require.Equal(t, tt.want, output.String())
		})
	}
}
