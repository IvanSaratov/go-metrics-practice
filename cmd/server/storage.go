package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
)

// Выбирает хранилище; PostgreSQL имеет приоритет над файловыми настройками.
func newStorage(config serverConfig, database *sql.DB) (repository.Storage, error) {
	if database != nil {
		return repository.NewPostgresStorage(database), nil
	}

	path := strings.TrimSpace(config.fileStoragePath)
	if path == "" {
		return repository.NewMemStorage(), nil
	}

	// При нулевом интервале сохраняем изменения после каждого обновления.
	storage := repository.NewFileStorage(path, config.storeInterval == 0)
	if config.restore {
		if err := storage.Restore(); err != nil {
			return nil, fmt.Errorf("restore metrics: %w", err)
		}
	}

	return storage, nil
}
