package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestServerAppUsesDefaultAddress(t *testing.T) {
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"server"})

	require.NoError(t, err)
	require.Equal(t, "localhost:8080", got.address)
}

func TestServerAppParsesAddressFlag(t *testing.T) {
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"server", "-a", "localhost:9090"})

	require.NoError(t, err)
	require.Equal(t, "localhost:9090", got.address)
}

func TestServerAppParsesEnv(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		env         map[string]string
		wantAddress string
	}{
		{
			name: "env overrides default",
			args: []string{"server"},
			env: map[string]string{
				"ADDRESS": "localhost:9090",
			},
			wantAddress: "localhost:9090",
		},
		{
			name: "flag overrides env",
			args: []string{"server", "-a", "localhost:7777"},
			env: map[string]string{
				"ADDRESS": "localhost:9090",
			},
			wantAddress: "localhost:7777",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				got = config
				return nil
			})

			err := app.Run(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.wantAddress, got.address)
		})
	}
}

func TestServerAppRejectsUnknownFlag(t *testing.T) {
	app := newServerApp(func(config serverConfig) error {
		return nil
	})

	err := app.Run([]string{"server", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}

func TestRunServerReturnsListenError(t *testing.T) {
	core, _ := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	err := runServer(context.Background(), serverConfig{address: "127.0.0.1:-1"}, log)

	require.Error(t, err)
	require.ErrorContains(t, err, "listen on 127.0.0.1:-1")
}

func TestRunLogsApplicationError(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	exitCode := run([]string{"server", "-unknown"}, log)

	require.Equal(t, 1, exitCode)

	errorEntries := observedLogs.FilterLevelExact(zapcore.ErrorLevel).All()
	require.Len(t, errorEntries, 1)
	require.Equal(t, "server failed", errorEntries[0].Message)
	require.Contains(t, errorEntries[0].ContextMap(), "error")
}

func TestRunServerShutsDownWhenContextCancelled(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runServer(ctx, serverConfig{address: "127.0.0.1:0"}, log)

	require.NoError(t, err)
	infoEntries := observedLogs.FilterLevelExact(zapcore.InfoLevel).All()
	messages := make([]string, 0, len(infoEntries))
	for _, entry := range infoEntries {
		messages = append(messages, entry.Message)
	}
	require.Equal(t, []string{
		"server started",
		"shutdown requested",
		"server stopped",
	}, messages)
}
