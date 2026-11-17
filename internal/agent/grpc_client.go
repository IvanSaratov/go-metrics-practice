package agent

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCClient отправляет батчи по gRPC. Его можно использовать из нескольких воркеров.
type GRPCClient struct {
	connection *grpc.ClientConn
	client     pb.MetricsClient
	timeout    time.Duration
}

// NewGRPCClient создаёт клиент, соединение устанавливается при первом запросе.
// timeout ограничивает время одного батча. При timeout <= 0 действует только контекст вызова.
func NewGRPCClient(address string, timeout time.Duration) (*GRPCClient, error) {
	connection, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(realIPCredentials{}),
		// Повтор по статусу RPC опасен для counter: сервер мог уже сохранить батч.
		grpc.WithDisableRetry(),
	)
	if err != nil {
		return nil, fmt.Errorf("create gRPC client: %w", err)
	}
	return &GRPCClient{
		connection: connection,
		client:     pb.NewMetricsClient(connection),
		timeout:    timeout,
	}, nil
}

// SendBatch отправляет все метрики одним запросом.
func (c *GRPCClient) SendBatch(ctx context.Context, metrics []model.Metrics) error {
	// Пустой батч не отправляется.
	if len(metrics) == 0 {
		return nil
	}
	if err := model.ValidateUpdates(metrics); err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid metrics batch: %v", err)
	}
	request := &pb.UpdateMetricsRequest{Metrics: make([]*pb.Metric, len(metrics))}
	for index, metric := range metrics {
		item := &pb.Metric{Id: metric.ID}
		switch metric.MType {
		case model.Gauge:
			if math.IsNaN(*metric.Value) || math.IsInf(*metric.Value, 0) {
				return status.Error(codes.InvalidArgument, "gauge value must be finite")
			}
			item.Type = pb.Metric_GAUGE
			item.Value = *metric.Value
		case model.Counter:
			item.Type = pb.Metric_COUNTER
			item.Delta = *metric.Delta
		}
		request.Metrics[index] = item
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	// Адрес добавится из выбранного соединения. Убираем чужие значения, чтобы не было дублей.
	md, _ := metadata.FromOutgoingContext(ctx)
	md.Delete("x-real-ip")
	ctx = metadata.NewOutgoingContext(ctx, md)
	if _, err := c.client.UpdateMetrics(ctx, request); err != nil {
		return fmt.Errorf("send metrics over gRPC: %w", err)
	}
	return nil
}

// Close закрывает соединение после остановки воркеров и финальной отправки.
func (c *GRPCClient) Close() error {
	return c.connection.Close()
}
