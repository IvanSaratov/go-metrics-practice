package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrustedSubnetMiddleware(t *testing.T) {
	for _, tt := range []struct {
		name    string
		subnet  string
		headers []string
		allowed bool
	}{
		{name: "disabled without header", allowed: true},
		{name: "disabled with invalid header", headers: []string{"invalid"}, allowed: true},
		{name: "IPv4 inside", subnet: "192.168.1.0/24", headers: []string{"192.168.1.5"}, allowed: true},
		{name: "IPv4 outside", subnet: "192.168.1.0/24", headers: []string{"192.168.2.5"}},
		{name: "missing header", subnet: "192.168.1.0/24"},
		{name: "invalid IP", subnet: "192.168.1.0/24", headers: []string{"invalid"}},
		{name: "IP with port", subnet: "192.168.1.0/24", headers: []string{"192.168.1.5:12345"}},
		{name: "comma separated IPs", subnet: "192.168.1.0/24", headers: []string{"192.168.1.5, 10.0.0.1"}},
		{name: "multiple headers", subnet: "192.168.1.0/24", headers: []string{"192.168.1.5", "10.0.0.1"}},
		{name: "IPv6 inside", subnet: "2001:db8::/32", headers: []string{"2001:db8::5"}, allowed: true},
		{name: "IPv6 outside", subnet: "2001:db8::/32", headers: []string{"2001:db9::5"}},
		{name: "different IP family", subnet: "2001:db8::/32", headers: []string{"192.168.1.5"}},
		{name: "IPv4 mapped IPv6", subnet: "192.168.1.0/24", headers: []string{"::ffff:192.168.1.5"}, allowed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var subnet *net.IPNet
			if tt.subnet != "" {
				var err error
				_, subnet, err = net.ParseCIDR(tt.subnet)
				require.NoError(t, err)
			}
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, "/updates", nil)
			// Адрес TCP-соединения не должен подменять отсутствующий X-Real-IP.
			request.RemoteAddr = "192.168.1.5:12345"
			for _, header := range tt.headers {
				request.Header.Add("X-Real-IP", header)
			}
			response := httptest.NewRecorder()

			TrustedSubnetMiddleware(subnet)(next).ServeHTTP(response, request)

			require.Equal(t, tt.allowed, called)
			if tt.allowed {
				require.Equal(t, http.StatusNoContent, response.Code)
			} else {
				require.Equal(t, http.StatusForbidden, response.Code)
			}
		})
	}
}
