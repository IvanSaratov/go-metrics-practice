package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	applogger "github.com/IvanSaratov/go-metrics-practice/internal/logger"
	"go.uber.org/zap"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

func main() {
	// Если данные сборки не передали, выводим N/A.
	if buildVersion == "" {
		buildVersion = "N/A"
	}
	if buildDate == "" {
		buildDate = "N/A"
	}
	if buildCommit == "" {
		buildCommit = "N/A"
	}
	fmt.Printf("Build version: %s\n", buildVersion)
	fmt.Printf("Build date: %s\n", buildDate)
	fmt.Printf("Build commit: %s\n", buildCommit)

	runMain()
}

func runMain() {
	appLogger, err := applogger.New(false)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "create logger: %v\n", err)
		os.Exit(1)
	}

	exitCode := run(os.Args, appLogger)
	// Ошибка Sync не должна менять код выхода.
	_ = appLogger.Sync()
	os.Exit(exitCode)
}

func run(args []string, appLogger *zap.Logger) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := newServerApp(func(config serverConfig) error {
		return runServer(ctx, config, appLogger)
	})

	if err := app.Run(args); err != nil {
		appLogger.Error("server failed", zap.Error(err))
		return 1
	}

	return 0
}
