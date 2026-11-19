package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	pb "github.com/IvanSaratov/go-metrics-practice/internal/proto"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTrustedSubnetInterceptor(t *testing.T) {
	for _, tt := range []struct {
		name    string
		subnet  string
		values  []string
		allowed bool
	}{
		{name: "disabled without metadata", allowed: true},
		{name: "disabled with invalid IP", values: []string{"invalid"}, allowed: true},
		{name: "IPv4 inside", subnet: "192.168.1.0/24", values: []string{"192.168.1.5"}, allowed: true},
		{name: "IPv4 outside", subnet: "192.168.1.0/24", values: []string{"192.168.2.5"}},
		{name: "missing metadata", subnet: "127.0.0.0/8"},
		{name: "empty IP", subnet: "192.168.1.0/24", values: []string{""}},
		{name: "invalid IP", subnet: "192.168.1.0/24", values: []string{"invalid"}},
		{name: "IP with port", subnet: "192.168.1.0/24", values: []string{"192.168.1.5:12345"}},
		{name: "comma separated IPs", subnet: "192.168.1.0/24", values: []string{"192.168.1.5, 10.0.0.1"}},
		{name: "multiple values", subnet: "192.168.1.0/24", values: []string{"192.168.1.5", "10.0.0.1"}},
		{name: "IPv6 inside", subnet: "2001:db8::/32", values: []string{"2001:db8::5"}, allowed: true},
		{name: "IPv6 outside", subnet: "2001:db8::/32", values: []string{"2001:db9::5"}},
		{name: "different IP family", subnet: "2001:db8::/32", values: []string{"192.168.1.5"}},
		{name: "IPv4 mapped IPv6", subnet: "192.168.1.0/24", values: []string{"::ffff:192.168.1.5"}, allowed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var subnet *net.IPNet
			if tt.subnet != "" {
				var err error
				_, subnet, err = net.ParseCIDR(tt.subnet)
				require.NoError(t, err)
			}
			storage := repository.NewMemStorage()
			_, err := storage.AddCounter(t.Context(), "requests", 10)
			require.NoError(t, err)
			events := make(chan audit.Event, 1)
			client := newTestClient(t, storage, func(_ context.Context, event audit.Event) { events <- event }, subnet)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if tt.values != nil {
				ctx = metadata.NewOutgoingContext(ctx, metadata.MD{"x-real-ip": tt.values})
			}

			response, err := client.UpdateMetrics(ctx, pb.UpdateMetricsRequest_builder{Metrics: []*pb.Metric{
				pb.Metric_builder{Id: "requests", Type: pb.Metric_COUNTER, Delta: 5}.Build(),
			}}.Build())

			wantCounter := int64(10)
			if tt.allowed {
				require.NoError(t, err)
				require.NotNil(t, response)
				wantCounter = 15
				require.Len(t, events, 1)
			} else {
				require.Equal(t, codes.PermissionDenied, status.Code(err))
				require.Nil(t, response)
				require.Empty(t, events)
			}
			counter, found, err := storage.GetCounter(t.Context(), "requests")
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, wantCounter, counter)
		})
	}
}

func TestTrustedSubnetInterceptorRunsBeforeValidation(t *testing.T) {
	_, subnet, err := net.ParseCIDR("192.168.1.0/24")
	require.NoError(t, err)
	client := newTestClient(t, repository.NewMemStorage(), nil, subnet)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request := pb.UpdateMetricsRequest_builder{Metrics: []*pb.Metric{
		pb.Metric_builder{Id: "bad", Type: pb.Metric_MType(99)}.Build(),
	}}.Build()

	_, err = client.UpdateMetrics(ctx, request)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	ctx = metadata.AppendToOutgoingContext(ctx, "x-real-ip", "192.168.1.5")
	_, err = client.UpdateMetrics(ctx, request)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
