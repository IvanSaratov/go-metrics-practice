package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	log "github.com/sirupsen/logrus"
)

func main() {
	storage := repository.NewMemStorage()
	router := handler.NewRouter(storage)

	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Info("starting server on :8080")

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Fatal("server failed")
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.WithError(err).Fatal("server shutdown failed")
	}

	log.Info("server stopped")
}
