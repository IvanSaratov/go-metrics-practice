package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Записывает события аудита в открытый файл.
type FileObserver struct {
	mu   sync.Mutex
	file io.WriteCloser
}

// Принимает открытый для дописывания файл и отвечает за его закрытие.
func NewFileObserver(file io.WriteCloser) *FileObserver {
	return &FileObserver{file: file}
}

// Добавляет событие одной JSON-строкой, сохраняя предыдущие записи.
func (o *FileObserver) Update(_ context.Context, event Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := json.NewEncoder(o.file).Encode(event); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

// Ожидает текущую запись и закрывает файл.
func (o *FileObserver) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.file.Close(); err != nil {
		return fmt.Errorf("close audit file: %w", err)
	}
	return nil
}
