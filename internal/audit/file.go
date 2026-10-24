package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// FileObserver записывает события аудита в файл.
// Можно использовать из нескольких горутин.
type FileObserver struct {
	mu   sync.Mutex
	file io.WriteCloser
}

// NewFileObserver создаёт приёмник для открытого файла.
// Файл должен быть открыт для дописывания. Его закрывает FileObserver.Close.
func NewFileObserver(file io.WriteCloser) *FileObserver {
	return &FileObserver{file: file}
}

// Update записывает событие одной строкой JSON.
// Контекст не используется.
func (o *FileObserver) Update(_ context.Context, event Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := json.NewEncoder(o.file).Encode(event); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

// Close ждёт завершения текущей записи и закрывает файл.
func (o *FileObserver) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.file.Close(); err != nil {
		return fmt.Errorf("close audit file: %w", err)
	}
	return nil
}
