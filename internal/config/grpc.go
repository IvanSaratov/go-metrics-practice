package config

import (
	"fmt"
	"net"
	"strconv"
)

// ValidateGRPCAddress проверяет host:port без подключения к серверу.
// Пустой адрес отключает gRPC, порт 0 позволяет серверу выбрать свободный порт.
func ValidateGRPCAddress(address string) error {
	if address == "" {
		return nil
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid gRPC address %q: %w", address, err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 {
		return fmt.Errorf("invalid gRPC address %q: port must be between 0 and 65535", address)
	}
	return nil
}
