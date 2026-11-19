package middleware

import (
	"net"
	"net/http"
)

// TrustedSubnetMiddleware проверяет X-Real-IP.
func TrustedSubnetMiddleware(subnet *net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ожидаем один IP: список адресов и повторяющиеся заголовки неоднозначны.
			values := r.Header.Values("X-Real-IP")
			if len(values) != 1 || !subnet.Contains(net.ParseIP(values[0])) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
