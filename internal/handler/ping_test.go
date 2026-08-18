package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestPingHandlerReturnsOKWhenDatabaseIsAvailable(t *testing.T) {
	server := NewServer(
		repository.NewMemStorage(),
		pingFunc(func(context.Context) error { return nil }),
	)
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
}

func TestPingHandlerReturnsInternalServerErrorWhenDatabaseIsUnavailable(t *testing.T) {
	server := NewServer(
		repository.NewMemStorage(),
		pingFunc(func(context.Context) error { return errors.New("database unavailable") }),
	)
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestPingHandlerReturnsInternalServerErrorWithoutDatabase(t *testing.T) {
	server := NewServer(repository.NewMemStorage(), nil)
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()

	require.NotPanics(t, func() {
		server.ServeHTTP(response, request)
	})
	require.Equal(t, http.StatusInternalServerError, response.Code)
}
