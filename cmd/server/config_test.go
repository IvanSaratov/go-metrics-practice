package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerAppUsesDefaultAddress(t *testing.T) {
	clearServerConfigEnv(t)
	t.Setenv("KEY", "")
	t.Setenv("CRYPTO_KEY", "")
	t.Setenv("AUDIT_FILE", "")
	t.Setenv("AUDIT_URL", "")

	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run(t.Context(), []string{"server"})

	require.NoError(t, err)
	require.Equal(t, "localhost:8080", got.address)
	require.Equal(t, 300*time.Second, got.storeInterval)
	require.Equal(t, "./temp/metrics-db.json", got.fileStoragePath)
	require.True(t, got.restore)
	require.Empty(t, got.databaseDSN)
	require.Empty(t, got.key)
	require.Empty(t, got.cryptoKeyPath)
	require.Empty(t, got.auditFile)
	require.Empty(t, got.auditURL)
}

func TestServerAppParsesFlags(t *testing.T) {
	clearServerConfigEnv(t)
	// Флаги должны перекрывать окружение для всех настроек сервера.
	t.Setenv("ADDRESS", "localhost:7070")
	t.Setenv("STORE_INTERVAL", "20")
	t.Setenv("FILE_STORAGE_PATH", "./env/metrics.json")
	t.Setenv("RESTORE", "true")
	t.Setenv("DATABASE_DSN", "postgres://localhost/env-db")
	t.Setenv("KEY", "env-secret")
	t.Setenv("CRYPTO_KEY", "env-private.pem")
	t.Setenv("AUDIT_FILE", "./env/audit.jsonl")
	t.Setenv("AUDIT_URL", "https://example.com/env-audit")

	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run(t.Context(), []string{
		"server",
		"-a", "localhost:9090",
		"-i", "15",
		"-f", "./custom/metrics.json",
		"-r=false",
		"-d", "postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
		"-k", "flag-secret",
		"-crypto-key", "private.pem",
		"--audit-file", "./logs/audit.jsonl",
		"--audit-url", "http://localhost:9090/audit",
		"-config", writeServerConfigFile(t, serverJSONConfig),
	})

	require.NoError(t, err)
	require.Equal(t, "localhost:9090", got.address)
	require.Equal(t, 15*time.Second, got.storeInterval)
	require.Equal(t, "./custom/metrics.json", got.fileStoragePath)
	require.False(t, got.restore)
	require.Equal(
		t,
		"postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
		got.databaseDSN,
	)
	require.Equal(t, "flag-secret", got.key)
	require.Equal(t, "private.pem", got.cryptoKeyPath)
	require.Equal(t, "./logs/audit.jsonl", got.auditFile)
	require.Equal(t, "http://localhost:9090/audit", got.auditURL)
}

func TestServerAppParsesEnv(t *testing.T) {
	clearServerConfigEnv(t)
	t.Setenv("ADDRESS", "localhost:9090")
	t.Setenv("STORE_INTERVAL", "20")
	t.Setenv("FILE_STORAGE_PATH", "./env/metrics.json")
	t.Setenv("RESTORE", "false")
	t.Setenv(
		"DATABASE_DSN",
		"postgres://env-user:env-password@localhost:5432/env-db?sslmode=disable",
	)
	t.Setenv("KEY", "env-secret")
	t.Setenv("CRYPTO_KEY", "env-private.pem")
	t.Setenv("AUDIT_FILE", "./env/audit.jsonl")
	t.Setenv("AUDIT_URL", "https://example.com/env-audit")
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run(t.Context(), []string{"server", "-c", writeServerConfigFile(t, serverJSONConfig)})

	require.NoError(t, err)
	require.Equal(t, "localhost:9090", got.address)
	require.Equal(t, 20*time.Second, got.storeInterval)
	require.Equal(t, "./env/metrics.json", got.fileStoragePath)
	require.False(t, got.restore)
	require.Equal(
		t,
		"postgres://env-user:env-password@localhost:5432/env-db?sslmode=disable",
		got.databaseDSN,
	)
	require.Equal(t, "env-secret", got.key)
	require.Equal(t, "env-private.pem", got.cryptoKeyPath)
	require.Equal(t, "./env/audit.jsonl", got.auditFile)
	require.Equal(t, "https://example.com/env-audit", got.auditURL)
}

// Проверяем независимость приёмников и явное переопределение настроек.
func TestServerAppAuditConfiguration(t *testing.T) {
	clearServerConfigEnv(t)
	tests := []struct {
		name     string
		args     []string
		envFile  string
		envURL   string
		wantFile string
		wantURL  string
	}{
		{
			name:     "file flag only",
			args:     []string{"--audit-file", "./logs/audit.jsonl"},
			wantFile: "./logs/audit.jsonl",
		},
		{
			name:    "URL flag only",
			args:    []string{"--audit-url", "http://localhost:9090/audit"},
			wantURL: "http://localhost:9090/audit",
		},
		{
			name:     "file environment only",
			envFile:  "./env/audit.jsonl",
			wantFile: "./env/audit.jsonl",
		},
		{
			name:    "URL environment only",
			envURL:  "https://example.com/env-audit",
			wantURL: "https://example.com/env-audit",
		},
		{
			name:     "flags override environment",
			args:     []string{"--audit-file", "./logs/audit.jsonl", "--audit-url", "http://localhost:9090/audit"},
			envFile:  "./env/audit.jsonl",
			envURL:   "https://example.com/env-audit",
			wantFile: "./logs/audit.jsonl",
			wantURL:  "http://localhost:9090/audit",
		},
		{
			name:    "empty file flag overrides environment",
			args:    []string{"--audit-file="},
			envFile: "./env/audit.jsonl",
			envURL:  "https://example.com/env-audit",
			wantURL: "https://example.com/env-audit",
		},
		{
			name:     "empty URL flag overrides environment",
			args:     []string{"--audit-url="},
			envFile:  "./env/audit.jsonl",
			envURL:   "https://example.com/env-audit",
			wantFile: "./env/audit.jsonl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUDIT_FILE", tt.envFile)
			t.Setenv("AUDIT_URL", tt.envURL)

			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), append([]string{"server"}, tt.args...))

			require.NoError(t, err)
			require.Equal(t, tt.wantFile, got.auditFile)
			require.Equal(t, tt.wantURL, got.auditURL)
		})
	}
}

func TestServerAppKeyFlagsOverrideEnvironment(t *testing.T) {
	clearServerConfigEnv(t)
	t.Setenv("KEY", "env-secret")
	t.Setenv("CRYPTO_KEY", "env-private.pem")

	for _, tt := range []struct {
		name          string
		cryptoKeyPath string
	}{
		{name: "crypto key path", cryptoKeyPath: "flag-private.pem"},
		{name: "empty crypto key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), []string{"server", "-k", "flag-secret", "--crypto-key=" + tt.cryptoKeyPath})

			require.NoError(t, err)
			require.Equal(t, "flag-secret", got.key)
			require.Equal(t, tt.cryptoKeyPath, got.cryptoKeyPath)
		})
	}
}

func TestServerAppDatabaseDSNFlagOverridesEnvironment(t *testing.T) {
	clearServerConfigEnv(t)
	t.Setenv(
		"DATABASE_DSN",
		"postgres://env-user:env-password@localhost:5432/env-db?sslmode=disable",
	)
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run(t.Context(), []string{
		"server",
		"-d", "postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
	})

	require.NoError(t, err)
	require.Equal(
		t,
		"postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
		got.databaseDSN,
	)
}

func TestServerAppParsesDatabaseDSNFlag(t *testing.T) {
	clearServerConfigEnv(t)
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run(t.Context(), []string{
		"server",
		"--database-dsn",
		"postgres://url-user:url-password@localhost:5432/url-db?sslmode=disable",
	})

	require.NoError(t, err)
	require.Equal(
		t,
		"postgres://url-user:url-password@localhost:5432/url-db?sslmode=disable",
		got.databaseDSN,
	)
}

func TestServerAppStoreInterval(t *testing.T) {
	clearServerConfigEnv(t)
	t.Setenv("STORE_INTERVAL", "20")

	for _, tt := range []struct {
		value   string
		wantErr string
	}{
		{value: "0"},
		{value: "-1", wantErr: "store interval must not be negative"},
		{value: "-18446744073", wantErr: "invalid duration"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			runCalled := false
			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				runCalled = true
				got = config
				return nil
			})

			err := app.Run(t.Context(), []string{"server", "-i", tt.value})

			if tt.wantErr == "" {
				require.NoError(t, err)
				require.True(t, runCalled)
				require.Zero(t, got.storeInterval)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
				require.False(t, runCalled)
			}
		})
	}
}

func TestServerAppRejectsUnknownFlag(t *testing.T) {
	clearServerConfigEnv(t)
	app := newServerApp(func(config serverConfig) error {
		return nil
	})

	err := app.Run(t.Context(), []string{"server", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}

const serverJSONConfig = `{
	"address": "file:8080",
	"restore": true,
	"store_interval": "500ms",
	"store_file": "./file/metrics.json",
	"database_dsn": "postgres://localhost/file-db",
	"key": "file-secret",
	"crypto_key": "file-private.pem",
	"audit_file": "./file/audit.jsonl",
	"audit_url": "https://example.com/file-audit"
}`

func TestServerAppJSONConfig(t *testing.T) {
	defaults := serverConfig{
		address: "localhost:8080", storeInterval: 300 * time.Second,
		fileStoragePath: "./temp/metrics-db.json", restore: true,
	}
	partial := defaults
	partial.cryptoKeyPath = "file.pem"
	overridden := defaults
	overridden.storeInterval = 5 * time.Second

	for _, tt := range []struct {
		name string
		json string
		env  map[string]string
		args []string
		want serverConfig
	}{
		{
			name: "all options", json: serverJSONConfig,
			want: serverConfig{
				address: "file:8080", storeInterval: 500 * time.Millisecond,
				fileStoragePath: "./file/metrics.json", restore: true,
				databaseDSN: "postgres://localhost/file-db", key: "file-secret",
				cryptoKeyPath: "file-private.pem", auditFile: "./file/audit.jsonl",
				auditURL: "https://example.com/file-audit",
			},
		},
		{name: "empty object", json: `{}`, want: defaults},
		{name: "partial object", json: `{"crypto_key":"file.pem"}`, want: partial},
		{
			name: "zero false and empty strings",
			json: `{"address":"","store_interval":"0s","store_file":"","restore":false}`,
			want: serverConfig{},
		},
		{
			name: "empty environment overrides file", json: serverJSONConfig,
			env: map[string]string{
				"ADDRESS": "", "STORE_INTERVAL": "0", "FILE_STORAGE_PATH": "", "RESTORE": "",
				"DATABASE_DSN": "", "KEY": "", "CRYPTO_KEY": "", "AUDIT_FILE": "", "AUDIT_URL": "",
			},
			want: serverConfig{},
		},
		{
			name: "empty flags override file", json: serverJSONConfig,
			args: []string{"-a=", "-i", "0", "-f=", "-r=false", "-d=", "-k=", "-crypto-key=", "--audit-file=", "--audit-url="},
			want: serverConfig{},
		},
		{
			name: "flags override invalid values", json: `{"store_interval":"invalid","restore":"invalid"}`,
			args: []string{"-i", "5", "-r=true"}, want: overridden,
		},
		{
			name: "environment overrides negative interval", json: `{"store_interval":"-1s"}`,
			env: map[string]string{"STORE_INTERVAL": "5s"}, want: overridden,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearServerConfigEnv(t)
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			var got serverConfig
			runCalled := false
			app := newServerApp(func(config serverConfig) error {
				got = config
				runCalled = true
				return nil
			})
			args := []string{"server", "-config", writeServerConfigFile(t, tt.json)}

			err := app.Run(t.Context(), append(args, tt.args...))

			require.NoError(t, err)
			require.True(t, runCalled)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestServerAppConfigPath(t *testing.T) {
	file := writeServerConfigFile(t, `{"address":"file:8080"}`)
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
		{name: "config after another flag", args: []string{"-r=false", "-config", file}, want: "file:8080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearServerConfigEnv(t)
			t.Setenv("CONFIG", tt.env)
			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), append([]string{"server"}, tt.args...))

			require.NoError(t, err)
			require.Equal(t, tt.want, got.address)
		})
	}
}

func TestServerAppRejectsInvalidConfig(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string
		args    []string
		wantErr string
	}{
		{name: "missing file", file: filepath.Join(t.TempDir(), "missing.json")},
		{name: "directory", file: t.TempDir()},
		{name: "empty file", file: writeServerConfigFile(t, "")},
		{name: "invalid JSON", file: writeServerConfigFile(t, `{"address":`)},
		{name: "YAML is not JSON", file: writeServerConfigFile(t, "address: file:8080")},
		{name: "null", file: writeServerConfigFile(t, `null`)},
		{name: "array", file: writeServerConfigFile(t, `[]`)},
		{name: "scalar", file: writeServerConfigFile(t, `"file:8080"`)},
		{name: "duplicate keys", file: writeServerConfigFile(t, `{"address":"first","address":"second"}`)},
		{name: "invalid duration", file: writeServerConfigFile(t, `{"store_interval":"invalid"}`)},
		{name: "invalid bool", file: writeServerConfigFile(t, `{"restore":"invalid"}`)},
		{name: "negative interval", file: writeServerConfigFile(t, `{"store_interval":"-1s"}`), wantErr: "store interval must not be negative"},
		{
			name: "invalid file with all options overridden", file: writeServerConfigFile(t, `{`),
			args: []string{"-a=", "-i", "0", "-f=", "-r=false", "-d=", "-k=", "-crypto-key=", "--audit-file=", "--audit-url="},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearServerConfigEnv(t)
			runCalled := false
			app := newServerApp(func(config serverConfig) error {
				runCalled = true
				return nil
			})
			args := []string{"server", "-config", tt.file}

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

func TestServerAppConfigHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-config", "missing.json", "--help"}} {
		t.Run(args[0], func(t *testing.T) {
			clearServerConfigEnv(t)
			t.Setenv("CONFIG", filepath.Join(t.TempDir(), "missing.json"))
			runCalled := false
			app := newServerApp(func(config serverConfig) error {
				runCalled = true
				return nil
			})
			app.Writer = io.Discard

			err := app.Run(t.Context(), append([]string{"server"}, args...))

			require.NoError(t, err)
			require.False(t, runCalled)
		})
	}
}

func TestServerAppStoreFileEnvironment(t *testing.T) {
	file := writeServerConfigFile(t, `{"store_file":"file.json"}`)
	for _, tt := range []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{name: "file fallback", want: "file.json"},
		{name: "STORE_FILE", env: map[string]string{"STORE_FILE": "alias.json"}, want: "alias.json"},
		{name: "legacy name takes precedence", env: map[string]string{"STORE_FILE": "alias.json", "FILE_STORAGE_PATH": "legacy.json"}, want: "legacy.json"},
		{name: "empty legacy value", env: map[string]string{"STORE_FILE": "alias.json", "FILE_STORAGE_PATH": ""}, want: ""},
		{name: "flag overrides alias", env: map[string]string{"STORE_FILE": "alias.json"}, args: []string{"-f", "flag.json"}, want: "flag.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearServerConfigEnv(t)
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				got = config
				return nil
			})

			err := app.Run(t.Context(), append([]string{"server", "-c", file}, tt.args...))

			require.NoError(t, err)
			require.Equal(t, tt.want, got.fileStoragePath)
		})
	}
}

func clearServerConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"CONFIG", "ADDRESS", "STORE_INTERVAL", "FILE_STORAGE_PATH", "STORE_FILE", "RESTORE", "DATABASE_DSN", "KEY", "CRYPTO_KEY", "AUDIT_FILE", "AUDIT_URL"} {
		// Setenv восстановит окружение после теста, Unsetenv убирает источник.
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
}

func writeServerConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
