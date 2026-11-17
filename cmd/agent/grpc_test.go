package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/grpcserver"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAgentAppRunsGRPCAndStops(t *testing.T) {
	clearAgentConfigEnv(t)
	storage := repository.NewMemStorage()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, subnet, err := net.ParseCIDR("127.0.0.1/32")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.UnaryInterceptor(grpcserver.TrustedSubnetInterceptor(subnet)))
	pb.RegisterMetricsServer(server, grpcserver.NewService(storage, nil))
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, <-serverDone) })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	app := newAgentApp(func(config agentConfig) error { return runAgentWithContext(ctx, config) })
	go func() {
		done <- app.Run(ctx, []string{
			"agent", "--grpc-address", listener.Addr().String(), "-p", "5ms", "-r", "10ms", "-t", "1s",
			// HTTP-настройки не должны влиять на выбранный gRPC-транспорт.
			"-a", "127.0.0.1:1", "-k", "HTTP-secret", "--crypto-key", "unused-http-key.pem",
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Error("agent did not stop")
		}
	})
	require.Eventually(t, func() bool {
		counter, found, err := storage.GetCounter(t.Context(), "PollCount")
		return err == nil && found && counter > 0
	}, 2*time.Second, time.Millisecond)
	gauges, err := storage.GetAllGauges(t.Context())
	require.NoError(t, err)
	require.Contains(t, gauges, "Alloc")
}
