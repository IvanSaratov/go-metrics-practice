package agent

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPClientSetsRealIP(t *testing.T) {
	for _, tt := range []struct {
		name     string
		network  string
		ip       string
		redirect bool
	}{
		{name: "IPv4", network: "tcp4", ip: "127.0.0.1"},
		{name: "IPv6", network: "tcp6", ip: "::1"},
		{name: "redirect from IPv4 to IPv6", network: "tcp6", ip: "::1", redirect: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen(tt.network, net.JoinHostPort(tt.ip, "0"))
			if err != nil && tt.network == "tcp6" {
				t.Skipf("IPv6 is unavailable: %v", err)
			}
			require.NoError(t, err)
			type receivedRequest struct {
				realIPs    []string
				remoteAddr string
				method     string
			}
			received := make(chan receivedRequest, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- receivedRequest{r.Header.Values("X-Real-IP"), r.RemoteAddr, r.Method}
			}))
			require.NoError(t, server.Listener.Close())
			server.Listener = listener
			server.Start()
			defer server.Close()
			url := server.URL
			if tt.redirect {
				redirect := httptest.NewServer(http.RedirectHandler(server.URL, http.StatusTemporaryRedirect))
				defer redirect.Close()
				url = redirect.URL
			}
			client := NewHTTPClient(2 * time.Second)
			defer client.CloseIdleConnections()

			var previousAddr string
			for range 2 {
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader("{}"))
				require.NoError(t, err)
				request.Header.Set("X-Real-IP", "192.0.2.1")

				response, err := client.Do(request)
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, response.Body.Close())
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.StatusCode)

				got := <-received
				require.Equal(t, []string{tt.ip}, got.realIPs)
				require.Equal(t, http.MethodPost, got.method)
				host, _, err := net.SplitHostPort(got.remoteAddr)
				require.NoError(t, err)
				require.Equal(t, host, got.realIPs[0])
				// Обёртка меняет копию запроса, а соединение можно использовать снова.
				require.Equal(t, "192.0.2.1", request.Header.Get("X-Real-IP"))
				if previousAddr != "" {
					require.Equal(t, previousAddr, got.remoteAddr)
				}
				previousAddr = got.remoteAddr
			}
		})
	}
}
