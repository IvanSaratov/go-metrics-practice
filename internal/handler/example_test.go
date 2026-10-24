package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
)

func ExampleServer_text() {
	server := handler.NewServer(repository.NewMemStorage(), nil, nil)

	request := httptest.NewRequest(http.MethodPost, "/update/counter/PollCount/3", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code)

	request = httptest.NewRequest(http.MethodGet, "/value/counter/PollCount", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code, response.Body.String())

	// Output:
	// 200
	// 200 3
}

func ExampleServer_json() {
	server := handler.NewServer(repository.NewMemStorage(), nil, nil)

	body := strings.NewReader(`{"id":"Alloc","type":"gauge","value":1024}`)
	request := httptest.NewRequest(http.MethodPost, "/update", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code)

	body = strings.NewReader(`{"id":"Alloc","type":"gauge"}`)
	request = httptest.NewRequest(http.MethodPost, "/value", body)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code)
	fmt.Print(response.Body.String())

	// Output:
	// 200
	// 200
	// {"id":"Alloc","type":"gauge","value":1024}
}

func ExampleServer_batch() {
	server := handler.NewServer(repository.NewMemStorage(), nil, nil)

	body := strings.NewReader(`[{"id":"Alloc","type":"gauge","value":2048},{"id":"PollCount","type":"counter","delta":2}]`)
	request := httptest.NewRequest(http.MethodPost, "/updates", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code)
	fmt.Print(response.Body.String())

	// Output:
	// 200
	// [{"id":"Alloc","type":"gauge","value":2048},{"id":"PollCount","type":"counter","delta":2}]
}

func ExampleServer_list() {
	storage := repository.NewMemStorage()
	_ = storage.SetGauge(context.Background(), "Alloc", 1024)
	server := handler.NewServer(storage, nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code, response.Header().Get("Content-Type"))
	fmt.Println(strings.Contains(response.Body.String(), "<li>Alloc: 1024</li>"))

	// Output:
	// 200 text/html; charset=utf-8
	// true
}

func ExampleServer_ping() {
	server := handler.NewServer(repository.NewMemStorage(), exampleDB{}, nil)

	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	fmt.Println(response.Code)

	// Output:
	// 200
}

// Заглушка для примера /ping.
type exampleDB struct{}

func (exampleDB) PingContext(context.Context) error {
	return nil
}
