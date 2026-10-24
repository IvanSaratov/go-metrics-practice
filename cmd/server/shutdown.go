package main

import (
	"net/http"
	"sync"
)

// Учитывает активные запросы для безопасного освобождения ресурсов сервера.
type drainingHandler struct {
	next    http.Handler
	mu      sync.Mutex
	active  sync.WaitGroup
	stopped bool
}

// Запускает обработчик, если сервер ещё не завершает ожидание запросов.
func (h *drainingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	h.active.Add(1)
	h.mu.Unlock()
	defer h.active.Done()

	h.next.ServeHTTP(w, r)
}

// Запрещает новые вызовы и ожидает выхода уже запущенных обработчиков.
func (h *drainingHandler) wait() {
	h.mu.Lock()
	h.stopped = true
	h.mu.Unlock()
	h.active.Wait()
}
