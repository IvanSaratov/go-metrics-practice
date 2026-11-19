package grpcserver

import (
	"context"
	"errors"
	"math"
	"net"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Service сохраняет батчи через то же хранилище, что и HTTP-обработчики.
type Service struct {
	pb.UnimplementedMetricsServer
	storage     repository.Storage
	notifyAudit func(context.Context, audit.Event)
}

// NewService создаёт сервис.
func NewService(storage repository.Storage, notifyAudit func(context.Context, audit.Event)) *Service {
	return &Service{storage: storage, notifyAudit: notifyAudit}
}

// UpdateMetrics проверяет весь батч до записи. Пустой батч ничего не меняет.
func (s *Service) UpdateMetrics(ctx context.Context, request *pb.UpdateMetricsRequest) (*pb.UpdateMetricsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	requestMetrics := request.GetMetrics()
	if len(requestMetrics) == 0 {
		return &pb.UpdateMetricsResponse{}, nil
	}

	metrics := make([]model.Metrics, len(requestMetrics))
	for index, metric := range requestMetrics {
		if metric == nil {
			return nil, status.Error(codes.InvalidArgument, "metric is required")
		}
		metrics[index].ID = metric.GetId()
		// Нужное поле выбираем по типу метрики.
		switch metric.GetType() {
		case pb.Metric_GAUGE:
			// Protobuf допускает NaN и Inf, но JSON нет.
			value := metric.GetValue()
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, status.Error(codes.InvalidArgument, "gauge value must be finite")
			}
			metrics[index].MType = model.Gauge
			metrics[index].Value = &value
		case pb.Metric_COUNTER:
			delta := metric.GetDelta()
			metrics[index].MType = model.Counter
			metrics[index].Delta = &delta
		default:
			return nil, status.Error(codes.InvalidArgument, "unsupported metric type")
		}
	}
	if err := model.ValidateUpdates(metrics); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.storage.UpdateBatch(ctx, metrics); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, status.FromContextError(err).Err()
		}
		return nil, status.Error(codes.Internal, "failed to store metrics")
	}

	s.auditRequest(ctx, metrics)
	return &pb.UpdateMetricsResponse{}, nil
}

func (s *Service) auditRequest(ctx context.Context, metrics []model.Metrics) {
	if s.notifyAudit == nil {
		return
	}
	names := make([]string, len(metrics))
	for index, metric := range metrics {
		names[index] = metric.ID
	}
	// Как и в HTTP-аудите, берём адрес соединения без порта.
	var address string
	if remote, ok := peer.FromContext(ctx); ok && remote.Addr != nil {
		address = remote.Addr.String()
		if host, _, err := net.SplitHostPort(address); err == nil {
			address = host
		}
	}
	s.notifyAudit(ctx, audit.Event{
		Timestamp: time.Now().Unix(),
		Metrics:   names,
		IPAddress: address,
	})
}
