package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUpdateMetricsStoresBatchesAndNotifiesAudit(t *testing.T) {
	storage := repository.NewMemStorage()
	events := make(chan audit.Event, 2)
	client := newTestClient(t, storage, func(_ context.Context, event audit.Event) {
		events <- event
	}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// В аудите нужен адрес соединения, как и у HTTP, а не значение из метаданных.
	ctx = metadata.AppendToOutgoingContext(ctx, "x-real-ip", "192.0.2.10")
	started := time.Now().Unix()

	response, err := client.UpdateMetrics(ctx, &pb.UpdateMetricsRequest{Metrics: []*pb.Metric{
		{Id: "temperature", Type: pb.Metric_GAUGE, Value: 23.5},
		{Id: "requests", Type: pb.Metric_COUNTER, Delta: 10},
		{Id: "requests", Type: pb.Metric_COUNTER, Delta: 5},
		{Id: "idle", Type: pb.Metric_COUNTER, Delta: 0},
	}})
	require.NoError(t, err)
	require.NotNil(t, response)
	gauges, err := storage.GetAllGauges(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]float64{"temperature": 23.5}, gauges)
	counters, err := storage.GetAllCounters(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"requests": 15, "idle": 0}, counters)

	_, err = client.UpdateMetrics(ctx, &pb.UpdateMetricsRequest{Metrics: []*pb.Metric{
		{Id: "temperature", Type: pb.Metric_GAUGE, Value: 0},
		{Id: "requests", Type: pb.Metric_COUNTER, Delta: -2},
	}})
	require.NoError(t, err)
	gauges, err = storage.GetAllGauges(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]float64{"temperature": 0}, gauges)
	counters, err = storage.GetAllCounters(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"requests": 13, "idle": 0}, counters)

	for _, names := range [][]string{{"temperature", "requests", "requests", "idle"}, {"temperature", "requests"}} {
		select {
		case event := <-events:
			require.Equal(t, names, event.Metrics)
			require.Equal(t, "127.0.0.1", event.IPAddress)
			require.GreaterOrEqual(t, event.Timestamp, started)
			require.LessOrEqual(t, event.Timestamp, time.Now().Unix())
		case <-ctx.Done():
			t.Fatal("audit event was not received")
		}
	}
}

func TestUpdateMetricsRejectsInvalidBatchWithoutPartialUpdate(t *testing.T) {
	for _, tt := range []struct {
		name   string
		metric *pb.Metric
	}{
		{name: "nil metric"},
		{name: "empty id", metric: &pb.Metric{Type: pb.Metric_GAUGE, Value: 12}},
		{name: "unknown type", metric: &pb.Metric{Id: "bad", Type: pb.Metric_MType(99)}},
		{name: "NaN", metric: &pb.Metric{Id: "bad", Value: math.NaN()}},
		{name: "positive infinity", metric: &pb.Metric{Id: "bad", Value: math.Inf(1)}},
		{name: "negative infinity", metric: &pb.Metric{Id: "bad", Value: math.Inf(-1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			storage := repository.NewMemStorage()
			_, err := storage.AddCounter(ctx, "requests", 10)
			require.NoError(t, err)
			events := 0
			service := NewService(storage, func(context.Context, audit.Event) { events++ })

			response, err := service.UpdateMetrics(ctx, &pb.UpdateMetricsRequest{Metrics: []*pb.Metric{
				{Id: "requests", Type: pb.Metric_COUNTER, Delta: 5},
				{Id: "temperature", Type: pb.Metric_GAUGE, Value: 23.5},
				tt.metric,
			}})

			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Nil(t, response)
			counters, err := storage.GetAllCounters(ctx)
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"requests": 10}, counters)
			gauges, err := storage.GetAllGauges(ctx)
			require.NoError(t, err)
			require.Empty(t, gauges)
			require.Zero(t, events)
		})
	}
}

func TestUpdateMetricsRejectsNilRequest(t *testing.T) {
	service := NewService(repository.NewMemStorage(), nil)
	response, err := service.UpdateMetrics(t.Context(), nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Nil(t, response)
}

func TestUpdateMetricsAcceptsEmptyBatchWithoutStorageOrAudit(t *testing.T) {
	storage := &failingStorage{err: errors.New("storage is unavailable")}
	events := make(chan audit.Event, 1)
	client := newTestClient(t, storage, func(_ context.Context, event audit.Event) { events <- event }, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	response, err := client.UpdateMetrics(ctx, &pb.UpdateMetricsRequest{})

	require.NoError(t, err)
	require.NotNil(t, response)
	require.Empty(t, events)
}

func TestUpdateMetricsStorageErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "internal", err: errors.New("secret database details"), code: codes.Internal},
		{name: "canceled", err: fmt.Errorf("store: %w", context.Canceled), code: codes.Canceled},
		{name: "deadline", err: fmt.Errorf("store: %w", context.DeadlineExceeded), code: codes.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan audit.Event, 1)
			client := newTestClient(t, &failingStorage{err: tt.err}, func(_ context.Context, event audit.Event) {
				events <- event
			}, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			response, err := client.UpdateMetrics(ctx, &pb.UpdateMetricsRequest{Metrics: []*pb.Metric{
				{Id: "requests", Type: pb.Metric_COUNTER, Delta: 1},
			}})

			require.Equal(t, tt.code, status.Code(err))
			require.Nil(t, response)
			require.NotContains(t, err.Error(), "secret database details")
			require.Empty(t, events)
		})
	}
}

func TestUpdateMetricsCanceledContextDoesNotWrite(t *testing.T) {
	storage := repository.NewMemStorage()
	service := NewService(storage, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	response, err := service.UpdateMetrics(ctx, &pb.UpdateMetricsRequest{Metrics: []*pb.Metric{
		{Id: "requests", Type: pb.Metric_COUNTER, Delta: 1},
	}})

	require.Equal(t, codes.Canceled, status.Code(err))
	require.Nil(t, response)
	counters, err := storage.GetAllCounters(t.Context())
	require.NoError(t, err)
	require.Empty(t, counters)
}

type failingStorage struct {
	repository.Storage
	err error
}

func (s *failingStorage) UpdateBatch(context.Context, []model.Metrics) error {
	return s.err
}

func newTestClient(t *testing.T, storage repository.Storage, notify func(context.Context, audit.Event), subnet *net.IPNet) pb.MetricsClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.UnaryInterceptor(TrustedSubnetInterceptor(subnet)))
	pb.RegisterMetricsServer(server, NewService(storage, notify))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, <-done)
	})
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	return pb.NewMetricsClient(connection)
}
