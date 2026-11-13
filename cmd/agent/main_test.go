package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunReturnsExitCodes(t *testing.T) {
	clearAgentConfigEnv(t)

	for _, tt := range []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "help", args: []string{"agent", "--help"}, wantCode: 0},
		{name: "unknown flag", args: []string{"agent", "-unknown"}, wantCode: 1},
		{name: "invalid config", args: []string{"agent", "-l", "0"}, wantCode: 1},
		{name: "invalid poll interval", args: []string{"agent", "-p", "invalid"}, wantCode: 1},
		{name: "invalid report interval", args: []string{"agent", "-r", "invalid"}, wantCode: 1},
		{name: "invalid timeout", args: []string{"agent", "-t", "invalid"}, wantCode: 1},
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
	invalidKeyPath := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(invalidKeyPath, []byte("not a PEM key"), 0o600))

	for _, tt := range []struct {
		name     string
		args     []string
		wantCode int
		wantText string
	}{
		{name: "help", args: []string{"--help"}, wantCode: 0, wantText: "USAGE:"},
		{name: "unknown flag", args: []string{"-unknown"}, wantCode: 1, wantText: "flag provided but not defined"},
		{name: "missing crypto key", args: []string{"-crypto-key", filepath.Join(t.TempDir(), "missing.pem"), "-p", "1h", "-r", "1h"}, wantCode: 1, wantText: "load public key"},
		{name: "invalid crypto key", args: []string{"-crypto-key", invalidKeyPath, "-p", "1h", "-r", "1h"}, wantCode: 1, wantText: "load public key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext(ctx, binary, tt.args...)
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
	clearAgentConfigEnv(t)

	var got agentConfig
	app := newAgentApp(func(config agentConfig) error {
		got = config
		return nil
	})

	err := app.Run(t.Context(), []string{"agent"})

	require.NoError(t, err)
	require.Equal(t, "localhost:8080", got.serverAddress)
	require.Equal(t, 2*time.Second, got.pollInterval)
	require.Equal(t, 10*time.Second, got.reportInterval)
	require.Equal(t, 30*time.Second, got.timeout)
	require.Empty(t, got.key)
	require.Empty(t, got.cryptoKeyPath)
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
		wantCryptoKeyPath  string
		wantRateLimit      int
	}{
		{
			name:               "numeric poll interval and duration report interval",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "3", "-r", "7s", "-t", "20s", "-k", "flag-secret", "-crypto-key", "public.pem", "-l", "3"},
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
			wantTimeout:        20 * time.Second,
			wantKey:            "flag-secret",
			wantCryptoKeyPath:  "public.pem",
			wantRateLimit:      3,
		},
		{
			name:               "different intervals",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "1h", "-r", "2m", "-t", "5ms", "--key", "long-secret", "--crypto-key", "public.pem", "--rate-limit", "4"},
			wantPollInterval:   time.Hour,
			wantReportInterval: 2 * time.Minute,
			wantTimeout:        5 * time.Millisecond,
			wantKey:            "long-secret",
			wantCryptoKeyPath:  "public.pem",
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
			clearAgentConfigEnv(t)

			var got agentConfig
			app := newAgentApp(func(config agentConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), tt.args)

			require.NoError(t, err)
			require.Equal(t, "localhost:9090", got.serverAddress)
			require.Equal(t, tt.wantPollInterval, got.pollInterval)
			require.Equal(t, tt.wantReportInterval, got.reportInterval)
			require.Equal(t, tt.wantTimeout, got.timeout)
			require.Equal(t, tt.wantKey, got.key)
			require.Equal(t, tt.wantCryptoKeyPath, got.cryptoKeyPath)
			require.Equal(t, tt.wantRateLimit, got.rateLimit)
		})
	}
}

func TestAgentAppParsesEnvs(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		env                map[string]string
		json               string
		wantAddress        string
		wantPollInterval   time.Duration
		wantReportInterval time.Duration
		wantTimeout        time.Duration
		wantKey            string
		wantCryptoKeyPath  string
		wantRateLimit      int
	}{
		{
			name: "env overrides JSON",
			args: []string{"agent"},
			json: agentJSONConfig,
			env: map[string]string{
				"ADDRESS":         "localhost:9090",
				"POLL_INTERVAL":   "5s",
				"REPORT_INTERVAL": "15s",
				"TIMEOUT":         "20s",
				"KEY":             "env-secret",
				"CRYPTO_KEY":      "env-public.pem",
				"RATE_LIMIT":      "5",
			},
			wantAddress:        "localhost:9090",
			wantPollInterval:   5 * time.Second,
			wantReportInterval: 15 * time.Second,
			wantTimeout:        20 * time.Second,
			wantKey:            "env-secret",
			wantCryptoKeyPath:  "env-public.pem",
			wantRateLimit:      5,
		},
		{
			name: "flag overrides env and JSON",
			args: []string{"agent", "-a", "localhost:7777", "-p", "3", "-r", "7s", "-t", "10s", "-k", "flag-secret", "-crypto-key", "flag-public.pem", "-l", "3"},
			json: agentJSONConfig,
			env: map[string]string{
				"ADDRESS":         "localhost:9090",
				"POLL_INTERVAL":   "5s",
				"REPORT_INTERVAL": "15s",
				"TIMEOUT":         "20s",
				"KEY":             "env-secret",
				"CRYPTO_KEY":      "env-public.pem",
				"RATE_LIMIT":      "5",
			},
			wantAddress:        "localhost:7777",
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
			wantTimeout:        10 * time.Second,
			wantKey:            "flag-secret",
			wantCryptoKeyPath:  "flag-public.pem",
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
		{
			name: "empty crypto key overrides env",
			args: []string{"agent", "-crypto-key="},
			env: map[string]string{
				"CRYPTO_KEY": "env-public.pem",
			},
			wantAddress:        "localhost:8080",
			wantPollInterval:   2 * time.Second,
			wantReportInterval: 10 * time.Second,
			wantTimeout:        30 * time.Second,
			wantRateLimit:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAgentConfigEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			args := tt.args
			if tt.json != "" {
				args = append(args, "-c", writeAgentConfigFile(t, tt.json))
			}

			var got agentConfig
			app := newAgentApp(func(config agentConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), args)

			require.NoError(t, err)
			require.Equal(t, tt.wantAddress, got.serverAddress)
			require.Equal(t, tt.wantPollInterval, got.pollInterval)
			require.Equal(t, tt.wantReportInterval, got.reportInterval)
			require.Equal(t, tt.wantTimeout, got.timeout)
			require.Equal(t, tt.wantKey, got.key)
			require.Equal(t, tt.wantCryptoKeyPath, got.cryptoKeyPath)
			require.Equal(t, tt.wantRateLimit, got.rateLimit)
		})
	}
}

func TestAgentAppRejectsNonPositiveValues(t *testing.T) {
	for _, tt := range []struct {
		flag string
		env  string
		err  string
	}{
		{flag: "-l", env: "RATE_LIMIT", err: "rate limit must be positive"},
		{flag: "-p", env: "POLL_INTERVAL", err: "poll interval must be positive"},
		{flag: "-r", env: "REPORT_INTERVAL", err: "report interval must be positive"},
	} {
		for _, value := range []string{"0", "-1"} {
			for _, source := range []string{"flag", "env"} {
				t.Run(tt.env+"/"+value+"/"+source, func(t *testing.T) {
					clearAgentConfigEnv(t)
					args := []string{"agent"}
					if source == "env" {
						t.Setenv(tt.env, value)
					} else {
						args = append(args, tt.flag, value)
					}
					runCalled := false
					app := newAgentApp(func(config agentConfig) error {
						runCalled = true
						return nil
					})

					err := app.Run(t.Context(), args)

					require.ErrorContains(t, err, tt.err)
					require.False(t, runCalled)
				})
			}
		}
	}
}

func TestAgentAppRejectsUnknownFlag(t *testing.T) {
	clearAgentConfigEnv(t)
	app := newAgentApp(func(config agentConfig) error {
		return nil
	})

	err := app.Run(t.Context(), []string{"agent", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}

const agentJSONConfig = `{
	"address": "file:8080",
	"poll_interval": "500ms",
	"report_interval": "1500ms",
	"timeout": "3s",
	"key": "file-secret",
	"crypto_key": "file-public.pem",
	"rate_limit": 2
}`

func TestAgentAppJSONConfig(t *testing.T) {
	defaults := agentConfig{
		serverAddress: "localhost:8080", pollInterval: 2 * time.Second,
		reportInterval: 10 * time.Second, timeout: 30 * time.Second, rateLimit: 1,
	}
	partial := defaults
	partial.cryptoKeyPath = "file.pem"
	emptyValues := agentConfig{
		pollInterval: 500 * time.Millisecond, reportInterval: 1500 * time.Millisecond, rateLimit: 2,
	}

	for _, tt := range []struct {
		name string
		json string
		env  map[string]string
		args []string
		want agentConfig
	}{
		{
			name: "all options", json: agentJSONConfig,
			want: agentConfig{
				serverAddress: "file:8080", pollInterval: 500 * time.Millisecond,
				reportInterval: 1500 * time.Millisecond, timeout: 3 * time.Second,
				key: "file-secret", cryptoKeyPath: "file-public.pem", rateLimit: 2,
			},
		},
		{name: "empty object", json: `{}`, want: defaults},
		{name: "partial object", json: `{"crypto_key":"file.pem"}`, want: partial},
		{
			name: "zero timeout and empty strings",
			json: `{"address":"","poll_interval":"500ms","report_interval":"1500ms","timeout":"0s","key":"","crypto_key":"","rate_limit":2}`,
			want: emptyValues,
		},
		{
			name: "empty environment overrides file", json: agentJSONConfig,
			env:  map[string]string{"ADDRESS": "", "KEY": "", "CRYPTO_KEY": "", "TIMEOUT": "0"},
			want: emptyValues,
		},
		{
			name: "empty flags override file", json: agentJSONConfig,
			args: []string{"-a=", "-k=", "-crypto-key=", "-t", "0"},
			want: emptyValues,
		},
		{
			name: "flags override invalid values",
			json: `{"poll_interval":"invalid","report_interval":"invalid","timeout":"invalid","rate_limit":"invalid"}`,
			args: []string{"-p", "2", "-r", "10", "-t", "30", "-l", "1"}, want: defaults,
		},
		{
			name: "environment overrides nonpositive values",
			json: `{"poll_interval":"0s","report_interval":"-1s","rate_limit":0}`,
			env:  map[string]string{"POLL_INTERVAL": "2s", "REPORT_INTERVAL": "10s", "RATE_LIMIT": "1"}, want: defaults,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearAgentConfigEnv(t)
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			var got agentConfig
			runCalled := false
			app := newAgentApp(func(config agentConfig) error {
				got = config
				runCalled = true
				return nil
			})
			args := []string{"agent", "-config", writeAgentConfigFile(t, tt.json)}

			err := app.Run(t.Context(), append(args, tt.args...))

			require.NoError(t, err)
			require.True(t, runCalled)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAgentAppConfigPath(t *testing.T) {
	file := writeAgentConfigFile(t, `{"address":"file:8080"}`)
	missing := filepath.Join(t.TempDir(), "missing.json")
	for _, tt := range []struct {
		name string
		args []string
		env  string
		want string
	}{
		{name: "short flag", args: []string{"-c", file}, want: "file:8080"},
		{name: "long flag", args: []string{"-config", file}, want: "file:8080"},
		{name: "double dash", args: []string{"--config", file}, want: "file:8080"},
		{name: "environment", env: file, want: "file:8080"},
		{name: "flag overrides environment", env: missing, args: []string{"-c", file}, want: "file:8080"},
		{name: "empty flag disables file", env: missing, args: []string{"-config="}, want: "localhost:8080"},
		{name: "config after another flag", args: []string{"-p", "1s", "-config", file}, want: "file:8080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearAgentConfigEnv(t)
			t.Setenv("CONFIG", tt.env)
			var got agentConfig
			app := newAgentApp(func(config agentConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), append([]string{"agent"}, tt.args...))

			require.NoError(t, err)
			require.Equal(t, tt.want, got.serverAddress)
		})
	}
}

func TestAgentAppRejectsInvalidConfig(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string
		args    []string
		wantErr string
	}{
		{name: "missing file", file: filepath.Join(t.TempDir(), "missing.json")},
		{name: "directory", file: t.TempDir()},
		{name: "empty file", file: writeAgentConfigFile(t, "")},
		{name: "invalid JSON", file: writeAgentConfigFile(t, `{"address":`)},
		{name: "YAML is not JSON", file: writeAgentConfigFile(t, "address: file:8080")},
		{name: "null", file: writeAgentConfigFile(t, `null`)},
		{name: "array", file: writeAgentConfigFile(t, `[]`)},
		{name: "scalar", file: writeAgentConfigFile(t, `"file:8080"`)},
		{name: "duplicate keys", file: writeAgentConfigFile(t, `{"address":"first","address":"second"}`)},
		{name: "invalid poll interval", file: writeAgentConfigFile(t, `{"poll_interval":"invalid"}`)},
		{name: "invalid report interval", file: writeAgentConfigFile(t, `{"report_interval":"invalid"}`)},
		{name: "invalid timeout", file: writeAgentConfigFile(t, `{"timeout":"invalid"}`)},
		{name: "invalid rate limit", file: writeAgentConfigFile(t, `{"rate_limit":1.5}`)},
		{name: "zero poll interval", file: writeAgentConfigFile(t, `{"poll_interval":"0s"}`), wantErr: "poll interval must be positive"},
		{name: "negative poll interval", file: writeAgentConfigFile(t, `{"poll_interval":"-1s"}`), wantErr: "poll interval must be positive"},
		{name: "zero report interval", file: writeAgentConfigFile(t, `{"report_interval":"0s"}`), wantErr: "report interval must be positive"},
		{name: "negative report interval", file: writeAgentConfigFile(t, `{"report_interval":"-1s"}`), wantErr: "report interval must be positive"},
		{name: "zero rate limit", file: writeAgentConfigFile(t, `{"rate_limit":0}`), wantErr: "rate limit must be positive"},
		{name: "negative rate limit", file: writeAgentConfigFile(t, `{"rate_limit":-1}`), wantErr: "rate limit must be positive"},
		{
			name: "invalid file with all options overridden", file: writeAgentConfigFile(t, `{`),
			args: []string{"-a=", "-p", "2", "-r", "10", "-t", "30", "-k=", "-crypto-key=", "-l", "1"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearAgentConfigEnv(t)
			runCalled := false
			app := newAgentApp(func(config agentConfig) error {
				runCalled = true
				return nil
			})
			args := []string{"agent", "-config", tt.file}

			err := app.Run(t.Context(), append(args, tt.args...))

			wantErr := tt.wantErr
			if wantErr == "" {
				wantErr = tt.file
			}
			require.ErrorContains(t, err, wantErr)
			require.False(t, runCalled)
		})
	}
}

func TestAgentAppConfigHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-config", "missing.json", "--help"}} {
		t.Run(args[0], func(t *testing.T) {
			clearAgentConfigEnv(t)
			t.Setenv("CONFIG", filepath.Join(t.TempDir(), "missing.json"))
			runCalled := false
			app := newAgentApp(func(config agentConfig) error {
				runCalled = true
				return nil
			})
			app.Writer = io.Discard

			err := app.Run(t.Context(), append([]string{"agent"}, args...))

			require.NoError(t, err)
			require.False(t, runCalled)
		})
	}
}

func clearAgentConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"CONFIG", "ADDRESS", "POLL_INTERVAL", "REPORT_INTERVAL", "TIMEOUT", "KEY", "CRYPTO_KEY", "RATE_LIMIT"} {
		// Setenv восстановит окружение после теста, Unsetenv убирает источник.
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
}

func writeAgentConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
