package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileStorageSavesAndRestoresMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temp", "metrics-db.json")
	storage := NewFileStorage(path, true)

	require.NoError(t, storage.SetGauge("LastGC", 1257894000000000000))
	total, err := storage.AddCounter("NumGC", 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), total)

	restored := NewFileStorage(path, false)
	require.NoError(t, restored.Restore())

	gauge, gaugeOK := restored.GetGauge("LastGC")
	require.True(t, gaugeOK)
	require.Equal(t, float64(1257894000000000000), gauge)
	counter, counterOK := restored.GetCounter("NumGC")
	require.True(t, counterOK)
	require.Equal(t, int64(42), counter)
}

func TestFileStoragePeriodicModeWritesOnlyOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage := NewFileStorage(path, false)
	require.NoError(t, storage.SetGauge("TestGauge", 67.1))

	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, storage.Save())
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestFileStorageRestoreMissingFile(t *testing.T) {
	storage := NewFileStorage(
		filepath.Join(t.TempDir(), "missing", "metrics-db.json"),
		false,
	)

	require.NoError(t, storage.Restore())
}

func TestFileStorageRestoreRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"invalid":`), 0o600))

	err := NewFileStorage(path, false).Restore()

	require.Error(t, err)
	require.ErrorContains(t, err, "decode metrics")
}

func TestFileStorageFailedSynchronousSaveKeepsPreviousState(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedParent, []byte("file"), 0o600))
	storage := NewFileStorage(
		filepath.Join(blockedParent, "metrics-db.json"),
		true,
	)

	err := storage.SetGauge("TestGauge", 67.1)

	require.Error(t, err)
	_, ok := storage.GetGauge("TestGauge")
	require.False(t, ok)
}
