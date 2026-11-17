package cliflags

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDurationSet(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{
			name:  "parses duration",
			value: "500ms",
			want:  500 * time.Millisecond,
		},
		{
			name:  "parses seconds without unit",
			value: "7",
			want:  7 * time.Second,
		},
		{
			name:  "largest whole seconds",
			value: "9223372036",
			want:  9223372036 * time.Second,
		},
		{
			name:  "smallest whole seconds",
			value: "-9223372036",
			want:  -9223372036 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := NewDuration(time.Second)

			err := duration.Set(tt.value)

			require.NoError(t, err)
			require.Equal(t, tt.want, duration.Duration())
		})
	}
}

func TestDurationSetRejectsInvalidValue(t *testing.T) {
	for _, value := range []string{"invalid", "9223372037", "-9223372037", "18446744073", "-18446744073"} {
		t.Run(value, func(t *testing.T) {
			duration := NewDuration(time.Second)

			err := duration.Set(value)

			require.Error(t, err)
			require.Equal(t, time.Second, duration.Duration())
		})
	}
}
