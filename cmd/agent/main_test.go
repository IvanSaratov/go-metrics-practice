package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunReturnsExitCodes(t *testing.T) {
	for _, name := range []string{"ADDRESS", "POLL_INTERVAL", "REPORT_INTERVAL", "TIMEOUT", "KEY", "RATE_LIMIT"} {
		t.Setenv(name, "")
	}

	for _, tt := range []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "help", args: []string{"agent", "--help"}, wantCode: 0},
		{name: "unknown flag", args: []string{"agent", "-unknown"}, wantCode: 1},
		{name: "invalid config", args: []string{"agent", "-l", "0"}, wantCode: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantCode, run(tt.args))
		})
	}
}

func TestMainExitCodes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agent")
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)

	for _, tt := range []struct {
		name     string
		flag     string
		wantCode int
		wantText string
	}{
		{name: "help", flag: "--help", wantCode: 0, wantText: "USAGE:"},
		{name: "unknown flag", flag: "-unknown", wantCode: 1, wantText: "flag provided but not defined"},
	} {
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

func TestAgentAppUsesDefaults(t *testing.T) {
	t.Setenv("KEY", "")
	t.Setenv("RATE_LIMIT", "")

	var got agentConfig
	app := newAgentApp(func(config agentConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"agent"})

	require.NoError(t, err)
	require.Equal(t, "localhost:8080", got.serverAddress)
	require.Equal(t, 2*time.Second, got.pollInterval)
	require.Equal(t, 10*time.Second, got.reportInterval)
	require.Equal(t, 30*time.Second, got.timeout)
	require.Empty(t, got.key)
	require.Equal(t, 1, got.rateLimit)
}

func TestAgentAppParsesFlags(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		wantPollInterval   time.Duration
		wantReportInterval time.Duration
		wantTimeout        time.Duration
		wantKey            string
		wantRateLimit      int
	}{
		{
			name:               "numeric poll interval and duration report interval",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "3", "-r", "7s", "-t", "20s", "-k", "flag-secret", "-l", "3"},
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
			wantTimeout:        20 * time.Second,
			wantKey:            "flag-secret",
			wantRateLimit:      3,
		},
		{
			name:               "different intervals",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "1h", "-r", "2m", "-t", "5ms", "--key", "long-secret", "--rate-limit", "4"},
			wantPollInterval:   time.Hour,
			wantReportInterval: 2 * time.Minute,
			wantTimeout:        5 * time.Millisecond,
			wantKey:            "long-secret",
			wantRateLimit:      4,
		},
		{
			name:               "work aliases",
			args:               []string{"agent", "--address", "localhost:9090", "--poll_interval", "10s", "--report_interval", "10s", "--timeout", "10s"},
			wantPollInterval:   10 * time.Second,
			wantReportInterval: 10 * time.Second,
			wantTimeout:        10 * time.Second,
			wantRateLimit:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KEY", "")
			t.Setenv("RATE_LIMIT", "")

			var got agentConfig
			app := newAgentApp(func(config agentConfig) error {
				got = config
				return nil
			})

			err := app.Run(tt.args)

			require.NoError(t, err)
			require.Equal(t, "localhost:9090", got.serverAddress)
			require.Equal(t, tt.wantPollInterval, got.pollInterval)
			require.Equal(t, tt.wantReportInterval, got.reportInterval)
			require.Equal(t, tt.wantTimeout, got.timeout)
			require.Equal(t, tt.wantKey, got.key)
			require.Equal(t, tt.wantRateLimit, got.rateLimit)
		})
	}
}

func TestAgentAppParsesEnvs(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		env                map[string]string
		wantAddress        string
		wantPollInterval   time.Duration
		wantReportInterval time.Duration
		wantTimeout        time.Duration
		wantKey            string
		wantRateLimit      int
	}{
		{
			name: "env overrides defaults",
			args: []string{"agent"},
			env: map[string]string{
				"ADDRESS":         "localhost:9090",
				"POLL_INTERVAL":   "5s",
				"REPORT_INTERVAL": "15s",
				"TIMEOUT":         "20s",
				"KEY":             "env-secret",
				"RATE_LIMIT":      "5",
			},
			wantAddress:        "localhost:9090",
			wantPollInterval:   5 * time.Second,
			wantReportInterval: 15 * time.Second,
			wantTimeout:        20 * time.Second,
			wantKey:            "env-secret",
			wantRateLimit:      5,
		},
		{
			name: "flag overrides env",
			args: []string{"agent", "-a", "localhost:7777", "-p", "3", "-r", "7s", "-t", "10s", "-k", "flag-secret", "-l", "3"},
			env: map[string]string{
				"ADDRESS":         "localhost:9090",
				"POLL_INTERVAL":   "5s",
				"REPORT_INTERVAL": "15s",
				"TIMEOUT":         "20s",
				"KEY":             "env-secret",
				"RATE_LIMIT":      "5",
			},
			wantAddress:        "localhost:7777",
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
			wantTimeout:        10 * time.Second,
			wantKey:            "flag-secret",
			wantRateLimit:      3,
		},
		{
			name: "env bare numeric seconds",
			args: []string{"agent"},
			env: map[string]string{
				"POLL_INTERVAL":   "5",
				"REPORT_INTERVAL": "15",
				"TIMEOUT":         "20",
			},
			wantAddress:        "localhost:8080",
			wantPollInterval:   5 * time.Second,
			wantReportInterval: 15 * time.Second,
			wantTimeout:        20 * time.Second,
			wantRateLimit:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KEY", "")
			t.Setenv("RATE_LIMIT", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			var got agentConfig
			app := newAgentApp(func(config agentConfig) error {
				got = config
				return nil
			})

			err := app.Run(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.wantAddress, got.serverAddress)
			require.Equal(t, tt.wantPollInterval, got.pollInterval)
			require.Equal(t, tt.wantReportInterval, got.reportInterval)
			require.Equal(t, tt.wantTimeout, got.timeout)
			require.Equal(t, tt.wantKey, got.key)
			require.Equal(t, tt.wantRateLimit, got.rateLimit)
		})
	}
}

func TestAgentAppRejectsNonPositiveRateLimit(t *testing.T) {
	t.Setenv("RATE_LIMIT", "")

	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			runCalled := false
			app := newAgentApp(func(config agentConfig) error {
				runCalled = true
				return nil
			})

			err := app.Run([]string{"agent", "-l", value})

			require.ErrorContains(t, err, "rate limit must be positive")
			require.False(t, runCalled)
		})
	}
}

func TestAgentAppRejectsUnknownFlag(t *testing.T) {
	app := newAgentApp(func(config agentConfig) error {
		return nil
	})

	err := app.Run([]string{"agent", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}
