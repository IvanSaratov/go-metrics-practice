package grpcserver

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TrustedSubnetInterceptor проверяет x-real-ip.
func TrustedSubnetInterceptor(subnet *net.IPNet) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if subnet != nil {
			md, _ := metadata.FromIncomingContext(ctx)
			values := md.Get("x-real-ip")
			// Как и в HTTP, принимаем ровно один IP, без порта и списка адресов.
			if len(values) != 1 || !subnet.Contains(net.ParseIP(values[0])) {
				return nil, status.Error(codes.PermissionDenied, "agent IP is outside trusted subnet")
			}
		}
		return handler(ctx, req)
	}
}
