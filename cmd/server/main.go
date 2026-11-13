package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/IvanSaratov/go-metrics-practice/internal/buildinfo"
	applogger "github.com/IvanSaratov/go-metrics-practice/internal/logger"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

func main() {
	buildinfo.Print(os.Stdout, buildVersion, buildDate, buildCommit)

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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer stop()

	app := newServerApp(func(config serverConfig) error {
		return runServer(ctx, config, appLogger)
	})

	if err := app.Run(ctx, args); err != nil {
		appLogger.Error("server failed", zap.Error(err))
		return 1
	}

	return 0
}

// validateJSONConfig проверяет файл, поскольку altsrc скрывает ошибки чтения.
func validateJSONConfig(path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("config %q contains invalid JSON", path)
	}

	// JSON-источник altsrc использует YAML-парсер. Проверяем, что он примет объект.
	var config map[any]any
	if err := yaml.Unmarshal(data, &config); err != nil || config == nil {
		return fmt.Errorf("config %q must contain a JSON object without duplicate keys", path)
	}
	return nil
}
