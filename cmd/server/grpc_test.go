package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/agent"
	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRunServerGRPCSharesStorageAndAuditWithHTTP(t *testing.T) {
	for _, tt := range []struct {
		name    string
		subnet  string
		allowed bool
	}{
		{name: "trusted agent", subnet: "127.0.0.0/8", allowed: true},
		{name: "untrusted agent", subnet: "10.0.0.0/8"},
		{name: "no restriction", allowed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			config := serverConfig{
				address: "127.0.0.1:0", grpcAddress: "127.0.0.1:0", shutdownTimeout: time.Second,
				storeInterval: time.Hour, fileStoragePath: filepath.Join(dir, "metrics.json"),
				auditFile: filepath.Join(dir, "audit.jsonl"), key: "HTTP-secret",
			}
			if tt.subnet != "" {
				var err error
				_, config.trustedSubnet, err = net.ParseCIDR(tt.subnet)
				require.NoError(t, err)
			}
			core, logs := observer.New(zap.InfoLevel)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- runServer(ctx, config, zap.New(core)) }()
			stop := sync.OnceFunc(func() {
				cancel()
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(3 * time.Second):
					t.Fatal("server did not stop")
				}
			})
			t.Cleanup(stop)
			waitAddress := func(message string) string {
				t.Helper()
				require.Eventually(t, func() bool { return logs.FilterMessage(message).Len() > 0 }, 2*time.Second, time.Millisecond)
				return logs.FilterMessage(message).All()[0].ContextMap()["address"].(string)
			}
			httpAddress := waitAddress("server started")
			grpcAddress := waitAddress("gRPC server started")
			grpcClient, err := agent.NewGRPCClient(grpcAddress, time.Second)
			require.NoError(t, err)
			defer grpcClient.Close()
			httpClient := agent.NewHTTPClient(time.Second)
			defer httpClient.CloseIdleConnections()
			httpSender := agent.NewClient("http://"+httpAddress, httpClient, config.key, nil)
			value, delta := 67.1, int64(1)
			err = grpcClient.SendBatch(t.Context(), []model.Metrics{
				{ID: "Alloc", MType: model.Gauge, Value: &value},
				{ID: "requests", MType: model.Counter, Delta: &delta},
			})
			if tt.allowed {
				require.NoError(t, err)
			} else {
				require.Equal(t, codes.PermissionDenied, status.Code(err))
			}
			delta = 2
			err = httpSender.SendBatch(t.Context(), []model.Metrics{{ID: "requests", MType: model.Counter, Delta: &delta}})
			if tt.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "403")
			}
			response, err := httpClient.Get("http://" + httpAddress + "/value/counter/requests")
			require.NoError(t, err)
			body, readErr := io.ReadAll(response.Body)
			require.NoError(t, response.Body.Close())
			require.NoError(t, readErr)
			if tt.allowed {
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Equal(t, "3", string(body))
			} else {
				require.Equal(t, http.StatusNotFound, response.StatusCode)
			}

			stop()
			for _, address := range []string{httpAddress, grpcAddress} {
				listener, err := net.Listen("tcp", address)
				require.NoError(t, err, "port should be released on shutdown")
				require.NoError(t, listener.Close())
			}
			data, err := os.ReadFile(config.fileStoragePath)
			require.NoError(t, err)
			var stored []model.Metrics
			require.NoError(t, json.Unmarshal(data, &stored))
			file, err := os.Open(config.auditFile)
			require.NoError(t, err)
			defer file.Close()
			var events []audit.Event
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				var event audit.Event
				require.NoError(t, json.Unmarshal(scanner.Bytes(), &event))
				events = append(events, event)
			}
			require.NoError(t, scanner.Err())
			if tt.allowed {
				delta = 3
				require.ElementsMatch(t, []model.Metrics{
					{ID: "Alloc", MType: model.Gauge, Value: &value},
					{ID: "requests", MType: model.Counter, Delta: &delta},
				}, stored)
				require.Len(t, events, 2)
				require.Equal(t, []string{"Alloc", "requests"}, events[0].Metrics)
				require.Equal(t, []string{"requests"}, events[1].Metrics)
				require.Equal(t, "127.0.0.1", events[0].IPAddress)
			} else {
				require.Empty(t, stored)
				require.Empty(t, events)
			}
		})
	}
}

func TestRunServerGRPCListenFailureReleasesHTTPPort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	httpPort, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	httpAddress := httpPort.Addr().String()
	require.NoError(t, httpPort.Close())
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	err = runServer(ctx, serverConfig{
		address: httpAddress, grpcAddress: occupied.Addr().String(), shutdownTimeout: time.Second,
	}, zap.NewNop())

	require.ErrorContains(t, err, "listen gRPC")
	listener, err := net.Listen("tcp", httpAddress)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
}

func TestShutdownGRPCWithActiveRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		complete     bool
		ignoreCancel bool
	}{
		{name: "completed request", complete: true},
		{name: "stalled request"},
		{name: "handler ignores cancellation", ignoreCancel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &blockingMetricsService{started: make(chan struct{}), release: make(chan struct{}), ignoreCancel: tt.ignoreCancel}
			release := sync.OnceFunc(func() { close(service.release) })
			t.Cleanup(release)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := grpc.NewServer()
			pb.RegisterMetricsServer(server, service)
			serveDone := make(chan error, 1)
			go func() { serveDone <- server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); require.NoError(t, <-serveDone) })
			client, err := agent.NewGRPCClient(listener.Addr().String(), 3*time.Second)
			require.NoError(t, err)
			defer client.Close()
			rpcDone := make(chan error, 1)
			go func() {
				delta := int64(1)
				rpcDone <- client.SendBatch(t.Context(), []model.Metrics{{ID: "requests", MType: model.Counter, Delta: &delta}})
			}()
			select {
			case <-service.started:
			case <-time.After(time.Second):
				t.Fatal("RPC did not start")
			}
			timeout := 30 * time.Millisecond
			if tt.complete {
				timeout = 2 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- shutdownGRPC(ctx, server) }()
			if tt.complete {
				// Закрытие слушателя означает, что остановка уже началась.
				require.Eventually(t, func() bool {
					connection, err := net.DialTimeout("tcp", listener.Addr().String(), 50*time.Millisecond)
					if err != nil {
						return true
					}
					connection.Close()
					return false
				}, time.Second, time.Millisecond)
				select {
				case err := <-done:
					t.Fatalf("shutdown did not wait for the RPC: %v", err)
				default:
				}
				release()
				require.NoError(t, <-rpcDone)
				require.NoError(t, <-done)
			} else {
				select {
				case err := <-done:
					require.ErrorIs(t, err, context.DeadlineExceeded)
				case <-time.After(time.Second):
					t.Fatal("shutdown did not enforce timeout")
				}
				require.Error(t, <-rpcDone)
			}
		})
	}
}

type blockingMetricsService struct {
	pb.UnimplementedMetricsServer
	started      chan struct{}
	release      chan struct{}
	ignoreCancel bool
}

func (s *blockingMetricsService) UpdateMetrics(ctx context.Context, _ *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
	close(s.started)
	if s.ignoreCancel {
		<-s.release
		return &pb.UpdateMetricsResponse{}, nil
	}
	select {
	case <-s.release:
		return &pb.UpdateMetricsResponse{}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}
