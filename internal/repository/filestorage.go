package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

// FileStorage хранит метрики в памяти и сохраняет их в JSON-файл.
// В синхронном режиме при ошибке записи данные в памяти не меняются.
// Контекст в методах не используется.
type FileStorage struct {
	*MemStorage

	mu          sync.Mutex
	path        string
	synchronous bool
}

// NewFileStorage создаёт хранилище для файла path.
// При synchronous=true каждое обновление сохраняется в файл, иначе нужен вызов Save.
// Чтобы загрузить метрики из файла, вызовите Restore.
func NewFileStorage(path string, synchronous bool) *FileStorage {
	return &FileStorage{
		MemStorage:  NewMemStorage(),
		path:        path,
		synchronous: synchronous,
	}
}

// SetGauge записывает новое значение gauge.
func (s *FileStorage) SetGauge(ctx context.Context, name string, value float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.synchronous {
		return s.MemStorage.SetGauge(ctx, name, value)
	}

	// Публикуем новое состояние только после атомарной замены файла
	snapshot := s.MemStorage.snapshot()
	snapshot.gauges[name] = value
	if err := s.saveSnapshot(snapshot); err != nil {
		return err
	}
	s.MemStorage.replace(snapshot)
	return nil
}

// AddCounter прибавляет value к счётчику и возвращает новое значение.
func (s *FileStorage) AddCounter(
	ctx context.Context,
	name string,
	value int64,
) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.synchronous {
		return s.MemStorage.AddCounter(ctx, name, value)
	}

	snapshot := s.MemStorage.snapshot()
	total := snapshot.counters[name] + value
	snapshot.counters[name] = total
	if err := s.saveSnapshot(snapshot); err != nil {
		return 0, err
	}
	s.MemStorage.replace(snapshot)
	return total, nil
}

// UpdateBatch проверяет пакет и обновляет метрики.
// В синхронном режиме файл сохраняется один раз для всего пакета.
func (s *FileStorage) UpdateBatch(_ context.Context, metrics []models.Metrics) error {
	// Общий метод валидации интерфейса
	if err := models.ValidateUpdates(metrics); err != nil {
		return err
	}
	if len(metrics) == 0 {
		return nil
	}

	// Можно все сделать одной транзакцией
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := s.MemStorage.snapshot()
	applyBatch(snapshot, metrics)
	if s.synchronous {
		if err := s.saveSnapshot(snapshot); err != nil {
			return err
		}
	}
	s.MemStorage.replace(snapshot)
	return nil
}

// Save сохраняет метрики в JSON-файл.
// Старый файл заменяется после успешной записи нового.
func (s *FileStorage) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveSnapshot(s.MemStorage.snapshot())
}

// Restore загружает метрики из файла в память.
// Если файла нет, ничего не делает.
func (s *FileStorage) Restore() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.loadSnapshot()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	s.MemStorage.replace(snapshot)
	return nil
}

func (s *FileStorage) saveSnapshot(snapshot metricsSnapshot) error {
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}

	// Временный файл в той же директории позволяет заменить снимок атомарно
	file, err := os.CreateTemp(directory, ".metrics-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary metrics file: %w", err)
	}
	tempPath := file.Name()
	fileOpen := true
	removeTemporary := true
	defer func() {
		if fileOpen {
			_ = file.Close()
		}
		if removeTemporary {
			_ = os.Remove(tempPath)
		}
	}()

	if err := json.NewEncoder(file).Encode(snapshot.metrics()); err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync metrics file: %w", err)
	}
	closeErr := file.Close()
	fileOpen = false
	if closeErr != nil {
		return fmt.Errorf("close metrics file: %w", closeErr)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace metrics file: %w", err)
	}
	removeTemporary = false
	return nil
}

func (s *FileStorage) loadSnapshot() (metricsSnapshot, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return metricsSnapshot{}, fmt.Errorf("read metrics file: %w", err)
	}

	var metrics []models.Metrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		return metricsSnapshot{}, fmt.Errorf("decode metrics: %w", err)
	}
	if err := models.ValidateUpdates(metrics); err != nil {
		return metricsSnapshot{}, fmt.Errorf("decode metrics: %w", err)
	}

	snapshot := metricsSnapshot{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
	for _, metric := range metrics {
		switch metric.MType {
		case models.Gauge:
			snapshot.gauges[metric.ID] = *metric.Value
		case models.Counter:
			snapshot.counters[metric.ID] = *metric.Delta
		}
	}

	return snapshot, nil
}

func (s metricsSnapshot) metrics() []models.Metrics {
	metrics := make([]models.Metrics, 0, len(s.gauges)+len(s.counters))

	for name, value := range s.gauges {
		value := value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &value,
		})
	}
	for name, value := range s.counters {
		value := value
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &value,
		})
	}

	return metrics
}
