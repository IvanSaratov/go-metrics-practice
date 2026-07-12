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
	"github.com/urfave/cli/v2"
)

type serverConfig struct {
	address string
}

func main() {
	app := newServerApp(runServer)
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func newServerApp(run func(config serverConfig) error) *cli.App {
	app := cli.NewApp()
	app.Name = "server"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "a",
			Aliases: []string{"address"},
			EnvVars: []string{"ADDRESS"},
			Value:   "localhost:8080",
			Usage:   "HTTP server address",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		return run(serverConfig{
			address: ctx.String("a"),
		})
	}

	return app
}

func runServer(config serverConfig) error {
	storage := repository.NewMemStorage()
	router := handler.NewRouter(storage)

	server := &http.Server{
		Addr:    config.address,
		Handler: router,
	}

	go func() {
		log.Infof("starting server on %s", config.address)

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
	return nil
}
