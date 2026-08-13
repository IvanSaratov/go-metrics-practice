package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenDoesNotRequireAvailableDatabase(t *testing.T) {
	database, err := Open(
		"postgres://metrics:metrics@127.0.0.1:1/metrics?sslmode=disable&connect_timeout=1",
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.Error(t, database.PingContext(ctx))
}

func TestOpenRejectsMalformedDSN(t *testing.T) {
	database, err := Open("postgres://%")

	require.Error(t, err)
	require.Nil(t, database)
}
