package agent

import (
	"net"
	"net/http"
	"net/http/httptrace"
)

type realIPTransport struct {
	*http.Transport
}

func (t *realIPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// Меняем свою копию, чтобы не затронуть запрос вызывающего кода и его редиректы.
	request = request.Clone(request.Context())
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			// Берём адрес выбранного соединения: при нескольких интерфейсах важен маршрут к серверу.
			if address, ok := info.Conn.LocalAddr().(*net.TCPAddr); ok {
				request.Header.Set("X-Real-IP", address.IP.String())
			}
		},
	}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	return t.Transport.RoundTrip(request)
}
