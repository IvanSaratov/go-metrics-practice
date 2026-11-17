package main

import (
	"context"

	"google.golang.org/grpc"
)

// Даём активным RPC закончиться, а по таймауту закрываем соединения и отменяем их контексты.
func shutdownGRPC(ctx context.Context, server *grpc.Server) error {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.Stop()
		// Обработчик может игнорировать отмену: после принудительной остановки его уже не ждём.
		return ctx.Err()
	}
}
