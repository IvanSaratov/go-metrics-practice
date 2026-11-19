package agent

import (
	"context"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/grpcserver"
	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestGRPCClientSendsBatchAndRealIP(t *testing.T) {
	for _, tt := range []struct {
		name    string
		network string
		ip      string
		cidr    string
	}{
		{name: "IPv4", network: "tcp4", ip: "127.0.0.1", cidr: "127.0.0.1/32"},
		{name: "IPv6", network: "tcp6", ip: "::1", cidr: "::1/128"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			storage := repository.NewMemStorage()
			type receivedRequest struct {
				metadata metadata.MD
				address  string
			}
			received := make(chan receivedRequest, 2)
			record := func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				remote, _ := peer.FromContext(ctx)
				received <- receivedRequest{md, remote.Addr.String()}
				return handler(ctx, request)
			}
			_, subnet, err := net.ParseCIDR(tt.cidr)
			require.NoError(t, err)
			address := startTestGRPCServer(t, tt.network, net.JoinHostPort(tt.ip, "0"), grpcserver.NewService(storage, nil),
				record, grpcserver.TrustedSubnetInterceptor(subnet))
			client := newTestGRPCClient(t, address, time.Second)
			ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
				"x-real-ip", "192.0.2.1", "x-real-ip", "192.0.2.2", "trace-id", "test-trace",
			))
			value, zero, delta := 23.5, 0.0, int64(9007199254740993)
			batch := []model.Metrics{
				{ID: "temperature", MType: model.Gauge, Value: &value},
				{ID: "zero", MType: model.Gauge, Value: &zero},
				{ID: "requests", MType: model.Counter, Delta: &delta},
			}

			var previousAddress string
			for range 2 {
				require.NoError(t, client.SendBatch(ctx, batch))
				got := <-received
				require.Equal(t, []string{tt.ip}, got.metadata.Get("x-real-ip"))
				require.Equal(t, []string{"test-trace"}, got.metadata.Get("trace-id"))
				host, _, err := net.SplitHostPort(got.address)
				require.NoError(t, err)
				require.Equal(t, host, got.metadata.Get("x-real-ip")[0])
				if previousAddress != "" {
					require.Equal(t, previousAddress, got.address, "connection should be reused")
				}
				previousAddress = got.address
			}
			gauges, err := storage.GetAllGauges(ctx)
			require.NoError(t, err)
			require.Equal(t, map[string]float64{"temperature": 23.5, "zero": 0}, gauges)
			counters, err := storage.GetAllCounters(ctx)
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"requests": 18014398509481986}, counters)
			original, _ := metadata.FromOutgoingContext(ctx)
			require.Equal(t, []string{"192.0.2.1", "192.0.2.2"}, original.Get("x-real-ip"))
		})
	}
}

func TestGRPCClientSendsConcurrentBatches(t *testing.T) {
	storage := repository.NewMemStorage()
	_, subnet, err := net.ParseCIDR("127.0.0.1/32")
	require.NoError(t, err)
	address := startTestGRPCServer(t, "tcp4", "127.0.0.1:0", grpcserver.NewService(storage, nil),
		grpcserver.TrustedSubnetInterceptor(subnet))
	client := newTestGRPCClient(t, address, 5*time.Second)
	var workers errgroup.Group
	for range 20 {
		workers.Go(func() error {
			delta := int64(1)
			return client.SendBatch(t.Context(), []model.Metrics{{ID: "requests", MType: model.Counter, Delta: &delta}})
		})
	}
	require.NoError(t, workers.Wait())
	counters, err := storage.GetAllCounters(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"requests": 20}, counters)
}

func TestGRPCClientRejectsInvalidBatchBeforeSending(t *testing.T) {
	var requests atomic.Int32
	service := &testMetricsService{update: func(context.Context, *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
		requests.Add(1)
		return &pb.UpdateMetricsResponse{}, nil
	}}
	address := startTestGRPCServer(t, "tcp4", "127.0.0.1:0", service)
	client := newTestGRPCClient(t, address, time.Second)
	value, delta, nan, infinity := 1.0, int64(1), math.NaN(), math.Inf(1)
	for _, tt := range []struct {
		name   string
		metric model.Metrics
	}{
		{name: "empty id", metric: model.Metrics{MType: model.Gauge, Value: &value}},
		{name: "unknown type", metric: model.Metrics{ID: "bad", MType: "unknown"}},
		{name: "missing gauge value", metric: model.Metrics{ID: "bad", MType: model.Gauge}},
		{name: "missing counter delta", metric: model.Metrics{ID: "bad", MType: model.Counter}},
		{name: "both values", metric: model.Metrics{ID: "bad", MType: model.Gauge, Value: &value, Delta: &delta}},
		{name: "NaN", metric: model.Metrics{ID: "bad", MType: model.Gauge, Value: &nan}},
		{name: "infinity", metric: model.Metrics{ID: "bad", MType: model.Gauge, Value: &infinity}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := client.SendBatch(t.Context(), []model.Metrics{
				{ID: "requests", MType: model.Counter, Delta: &delta}, tt.metric,
			})
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Zero(t, requests.Load())
		})
	}
}

func TestGRPCClientEmptyBatchAndClose(t *testing.T) {
	storage := repository.NewMemStorage()
	address := startTestGRPCServer(t, "tcp4", "127.0.0.1:0", grpcserver.NewService(storage, nil))
	client, err := NewGRPCClient(address, time.Second)
	require.NoError(t, err)
	// Close должен закрывать и уже установленное соединение.
	delta := int64(1)
	batch := []model.Metrics{{ID: "requests", MType: model.Counter, Delta: &delta}}
	sendErr := client.SendBatch(t.Context(), batch)
	require.NoError(t, client.Close())
	require.NoError(t, sendErr)

	require.NoError(t, client.SendBatch(t.Context(), nil))
	require.NoError(t, client.SendBatch(t.Context(), []model.Metrics{}))
	require.Error(t, client.SendBatch(t.Context(), batch))
	counters, err := storage.GetAllCounters(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"requests": 1}, counters)
}

func TestGRPCClientPreservesServerErrorWithoutRepeatingBatch(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unavailable, codes.Internal} {
		t.Run(code.String(), func(t *testing.T) {
			storage := repository.NewMemStorage()
			metricsService := grpcserver.NewService(storage, nil)
			service := &testMetricsService{update: func(ctx context.Context, request *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
				if code != codes.PermissionDenied {
					// Ошибка ответа не гарантирует, что сервер не успел прибавить counter.
					if _, err := metricsService.UpdateMetrics(ctx, request); err != nil {
						return nil, err
					}
				}
				return nil, status.Error(code, "request failed")
			}}
			address := startTestGRPCServer(t, "tcp4", "127.0.0.1:0", service)
			client := newTestGRPCClient(t, address, time.Second)
			delta := int64(3)

			err := client.SendBatch(t.Context(), []model.Metrics{{ID: "requests", MType: model.Counter, Delta: &delta}})

			require.Equal(t, code, status.Code(err))
			counters, err := storage.GetAllCounters(t.Context())
			require.NoError(t, err)
			if code == codes.PermissionDenied {
				require.Empty(t, counters)
			} else {
				require.Equal(t, map[string]int64{"requests": 3}, counters)
			}
		})
	}
}

func TestGRPCClientRespectsTimeoutAndContext(t *testing.T) {
	for _, tt := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
	}{
		{name: "client timeout", timeout: 250 * time.Millisecond},
		{name: "caller cancellation", timeout: 0, cancel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{}, 1)
			stopped := make(chan struct{}, 1)
			service := &testMetricsService{update: func(ctx context.Context, _ *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
				started <- struct{}{}
				<-ctx.Done()
				stopped <- struct{}{}
				return nil, status.FromContextError(ctx.Err()).Err()
			}}
			address := startTestGRPCServer(t, "tcp4", "127.0.0.1:0", service)
			client := newTestGRPCClient(t, address, tt.timeout)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				delta := int64(1)
				done <- client.SendBatch(ctx, []model.Metrics{{ID: "requests", MType: model.Counter, Delta: &delta}})
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("server did not receive the batch")
			}
			wantCode := codes.DeadlineExceeded
			if tt.cancel {
				cancel()
				wantCode = codes.Canceled
			}
			select {
			case err := <-done:
				require.Equal(t, wantCode, status.Code(err))
			case <-time.After(5 * time.Second):
				t.Fatal("SendBatch did not stop")
			}
			if !tt.cancel {
				require.NoError(t, ctx.Err(), "client should stop before the caller deadline")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("server request was not canceled")
			}
		})
	}
}

type testMetricsService struct {
	pb.UnimplementedMetricsServer
	update func(context.Context, *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error)
}

func (s *testMetricsService) UpdateMetrics(ctx context.Context, request *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
	return s.update(ctx, request)
}

func startTestGRPCServer(t *testing.T, network, address string, service pb.MetricsServer, interceptors ...grpc.UnaryServerInterceptor) string {
	t.Helper()
	listener, err := net.Listen(network, address)
	if err != nil && network == "tcp6" {
		t.Skipf("IPv6 is unavailable: %v", err)
	}
	require.NoError(t, err)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptors...))
	pb.RegisterMetricsServer(server, service)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, <-done)
	})
	return listener.Addr().String()
}

func newTestGRPCClient(t *testing.T, address string, timeout time.Duration) *GRPCClient {
	t.Helper()
	client, err := NewGRPCClient(address, timeout)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}
