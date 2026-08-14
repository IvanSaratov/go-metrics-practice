package repository

import (
	"os"
	"path/filepath"
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/stretchr/testify/require"
)

func TestFileStorageSavesAndRestoresMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temp", "metrics-db.json")
	storage := NewFileStorage(path, true)

	require.NoError(t, storage.SetGauge(testContext, "LastGC", 1257894000000000000))
	total, err := storage.AddCounter(testContext, "NumGC", 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), total)

	restored := NewFileStorage(path, false)
	require.NoError(t, restored.Restore())

	gauge, gaugeOK, err := restored.GetGauge(testContext, "LastGC")
	require.NoError(t, err)
	require.True(t, gaugeOK)
	require.Equal(t, float64(1257894000000000000), gauge)
	counter, counterOK, err := restored.GetCounter(testContext, "NumGC")
	require.NoError(t, err)
	require.True(t, counterOK)
	require.Equal(t, int64(42), counter)
}

func TestFileStoragePeriodicModeWritesOnlyOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage := NewFileStorage(path, false)
	require.NoError(t, storage.SetGauge(testContext, "TestGauge", 67.1))

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

func TestFileStorageRestoreRejectsInvalidMetric(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	require.NoError(t, os.WriteFile(
		path,
		[]byte(`[{"id":"temperature","type":"gauge"}]`),
		0o600,
	))

	err := NewFileStorage(path, false).Restore()

	require.Error(t, err)
}

func TestFileStorageFailedSynchronousSaveKeepsPreviousState(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedParent, []byte("file"), 0o600))
	storage := NewFileStorage(
		filepath.Join(blockedParent, "metrics-db.json"),
		true,
	)

	err := storage.SetGauge(testContext, "TestGauge", 67.1)

	require.Error(t, err)
	_, ok, readErr := storage.GetGauge(testContext, "TestGauge")
	require.NoError(t, readErr)
	require.False(t, ok)
}

func TestFileStorageUpdateBatchSavesConsistentSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage := NewFileStorage(path, true)

	err := storage.UpdateBatch(testContext, []models.Metrics{
		gaugeMetric("temperature", 23.5),
		counterMetric("requests", 10),
		counterMetric("requests", 5),
	})
	require.NoError(t, err)

	restored := NewFileStorage(path, false)
	require.NoError(t, restored.Restore())
	gauge, found, err := restored.GetGauge(testContext, "temperature")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 23.5, gauge)
	counter, found, err := restored.GetCounter(testContext, "requests")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(15), counter)
}

func TestFileStorageFailedBatchSaveKeepsPreviousState(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedParent, []byte("file"), 0o600))
	storage := NewFileStorage(filepath.Join(blockedParent, "metrics-db.json"), true)

	err := storage.UpdateBatch(testContext, []models.Metrics{
		gaugeMetric("temperature", 23.5),
		counterMetric("requests", 10),
	})

	require.Error(t, err)
	_, gaugeFound, readErr := storage.GetGauge(testContext, "temperature")
	require.NoError(t, readErr)
	require.False(t, gaugeFound)
	_, counterFound, readErr := storage.GetCounter(testContext, "requests")
	require.NoError(t, readErr)
	require.False(t, counterFound)
}
