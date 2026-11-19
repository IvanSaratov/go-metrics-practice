package agent

import (
	"context"
	"net"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// gRPC вызывает PerRPCCredentials после выбора соединения, когда уже известен его IP.
type realIPCredentials struct{}

func (realIPCredentials) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	connection, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, "gRPC connection address is unavailable")
	}
	address, ok := connection.LocalAddr.(*net.TCPAddr)
	if !ok || address.IP.To16() == nil {
		return nil, status.Error(codes.Internal, "gRPC connection has no local TCP IP")
	}
	return map[string]string{"x-real-ip": address.IP.String()}, nil
}

func (realIPCredentials) RequireTransportSecurity() bool {
	return false
}
