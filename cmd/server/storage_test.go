package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestNewStorageUsesPostgresWhenDatabaseProvided(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, database.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	storage, err := newStorage(serverConfig{
		fileStoragePath: filepath.Join(t.TempDir(), "metrics-db.json"),
		restore:         true,
	}, database)

	require.NoError(t, err)
	require.IsType(t, &repository.PostgresStorage{}, storage)
}

func TestNewStorageUsesFileWhenPathProvided(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage, err := newStorage(serverConfig{
		fileStoragePath: path,
		storeInterval:   0,
	}, nil)
	require.NoError(t, err)

	require.NoError(t, storage.SetGauge(context.Background(), "temperature", 23.5))
	restored := repository.NewFileStorage(path, false)
	require.NoError(t, restored.Restore())
	value, found, err := restored.GetGauge(context.Background(), "temperature")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 23.5, value)
}

func TestNewStorageUsesMemoryWhenFilePathEmpty(t *testing.T) {
	storage, err := newStorage(serverConfig{fileStoragePath: " \t "}, nil)

	require.NoError(t, err)
	require.IsType(t, &repository.MemStorage{}, storage)
}
