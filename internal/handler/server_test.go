package handler

import (
	"context"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
)

type pingFunc func(context.Context) error

func (f pingFunc) PingContext(ctx context.Context) error {
	return f(ctx)
}

func newTestServer(storage repository.Storage) *Server {
	return NewServer(
		storage,
		pingFunc(func(context.Context) error { return nil }),
	)
}
