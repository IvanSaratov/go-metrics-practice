package handler

import (
	"context"
	"net/http"
)

type Database interface {
	PingContext(context.Context) error
}

func (s *Server) ping(writer http.ResponseWriter, request *http.Request) {
	if err := s.db.PingContext(request.Context()); err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	writer.WriteHeader(http.StatusOK)
}
