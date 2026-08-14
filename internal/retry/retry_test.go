package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackoffReturnsRequiredIntervals(t *testing.T) {
	backoff := newBackoff()

	for _, expected := range []time.Duration{
		time.Second,
		3 * time.Second,
		5 * time.Second,
	} {
		delay, stop := backoff.Next()
		require.False(t, stop)
		require.Equal(t, expected, delay)
	}

	delay, stop := backoff.Next()
	require.True(t, stop)
	require.Zero(t, delay)
}

func TestDoStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	temporaryErr := errors.New("temporary error")

	err := Do(ctx, func(context.Context) error {
		attempts++
		cancel()
		return RetryableError(temporaryErr)
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}
