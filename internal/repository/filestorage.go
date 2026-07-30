package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

// FileStorage хранит актуальные метрики в памяти и сохраняет их снимки в JSON.
type FileStorage struct {
	mu          sync.Mutex
	memory      *MemStorage
	path        string
	synchronous bool
}

func NewFileStorage(path string, synchronous bool) *FileStorage {
	return &FileStorage{
		memory:      NewMemStorage(),
		path:        path,
		synchronous: synchronous,
	}
}

var _ Storage = (*FileStorage)(nil)

func (s *FileStorage) SetGauge(name string, value float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.synchronous {
		return s.memory.SetGauge(name, value)
	}

	// Публикуем новое состояние только после атомарной замены файла
	snapshot := s.memory.snapshot()
	snapshot.gauges[name] = value
	if err := s.saveSnapshot(snapshot); err != nil {
		return err
	}
	s.memory.replace(snapshot)
	return nil
}

func (s *FileStorage) AddCounter(name string, value int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.synchronous {
		return s.memory.AddCounter(name, value)
	}

	snapshot := s.memory.snapshot()
	total := snapshot.counters[name] + value
	snapshot.counters[name] = total
	if err := s.saveSnapshot(snapshot); err != nil {
		return 0, err
	}
	s.memory.replace(snapshot)
	return total, nil
}

func (s *FileStorage) GetGauge(name string) (float64, bool) {
	return s.memory.GetGauge(name)
}

func (s *FileStorage) GetCounter(name string) (int64, bool) {
	return s.memory.GetCounter(name)
}

func (s *FileStorage) GetAllGauges() map[string]float64 {
	return s.memory.GetAllGauges()
}

func (s *FileStorage) GetAllCounters() map[string]int64 {
	return s.memory.GetAllCounters()
}

func (s *FileStorage) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveSnapshot(s.memory.snapshot())
}

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

	s.memory.replace(snapshot)
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
	removeTemporary := true
	defer func() {
		_ = file.Close()
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
	if err := file.Close(); err != nil {
		return fmt.Errorf("close metrics file: %w", err)
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

	snapshot := metricsSnapshot{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
	for _, metric := range metrics {
		switch metric.MType {
		case models.Gauge:
			if metric.ID == "" || metric.Value == nil || metric.Delta != nil {
				return metricsSnapshot{}, fmt.Errorf("decode metrics: invalid gauge %q", metric.ID)
			}
			snapshot.gauges[metric.ID] = *metric.Value
		case models.Counter:
			if metric.ID == "" || metric.Delta == nil || metric.Value != nil {
				return metricsSnapshot{}, fmt.Errorf("decode metrics: invalid counter %q", metric.ID)
			}
			snapshot.counters[metric.ID] = *metric.Delta
		default:
			return metricsSnapshot{}, fmt.Errorf(
				"decode metrics: unsupported metric type %q",
				metric.MType,
			)
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
