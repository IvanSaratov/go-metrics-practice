package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSaveMetricsPeriodicallyWritesOnTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage := repository.NewFileStorage(path, false)
	require.NoError(t, storage.SetGauge(context.Background(), "TestGauge", 67.1))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		saveMetricsPeriodically(ctx, ticks, storage, zap.NewNop())
	}()

	ticks <- time.Now()
	close(ticks)
	<-done

	restored := repository.NewFileStorage(path, false)
	require.NoError(t, restored.Restore())
	value, ok, err := restored.GetGauge(context.Background(), "TestGauge")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 67.1, value)
}
