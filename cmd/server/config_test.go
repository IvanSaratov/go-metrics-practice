package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerAppUsesDefaultAddress(t *testing.T) {
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

	err := app.Run(t.Context(), []string{"server"})

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
	t.Setenv("STORE_INTERVAL", "20")

	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			runCalled := false
			app := newServerApp(func(config serverConfig) error {
				runCalled = true
				require.Zero(t, config.storeInterval)
				return nil
			})

			err := app.Run(t.Context(), []string{"server", "-i", value})

			if value == "0" {
				require.NoError(t, err)
				require.True(t, runCalled)
			} else {
				require.ErrorContains(t, err, "store interval must not be negative")
				require.False(t, runCalled)
			}
		})
	}
}

func TestServerAppRejectsUnknownFlag(t *testing.T) {
	app := newServerApp(func(config serverConfig) error {
		return nil
	})

	err := app.Run(t.Context(), []string{"server", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}
