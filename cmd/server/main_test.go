package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestMainExitCodes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "server")
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)

	tests := []struct {
		name     string
		flag     string
		wantCode int
		wantText string
	}{
		{name: "help", flag: "--help", wantCode: 0, wantText: "USAGE:"},
		{name: "unknown flag", flag: "-unknown", wantCode: 1, wantText: "server failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext(ctx, binary, tt.flag)
			// Настройки из окружения не должны влиять на проверку флагов.
			command.Env = []string{}
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err())
			if tt.wantCode == 0 {
				require.NoError(t, err, "%s", output)
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				require.Equal(t, tt.wantCode, exitErr.ExitCode())
			}
			require.Contains(t, string(output), tt.wantText)
		})
	}
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
