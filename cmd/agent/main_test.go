package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentAppUsesDefaults(t *testing.T) {
	t.Setenv("KEY", "")

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
}

func TestAgentAppParsesFlags(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		wantPollInterval   time.Duration
		wantReportInterval time.Duration
		wantTimeout        time.Duration
		wantKey            string
	}{
		{
			name:               "numeric poll interval and duration report interval",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "3", "-r", "7s", "-t", "20s", "-k", "flag-secret"},
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
			wantTimeout:        20 * time.Second,
			wantKey:            "flag-secret",
		},
		{
			name:               "different intervals",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "1h", "-r", "2m", "-t", "5ms", "--key", "long-secret"},
			wantPollInterval:   time.Hour,
			wantReportInterval: 2 * time.Minute,
			wantTimeout:        5 * time.Millisecond,
			wantKey:            "long-secret",
		},
		{
			name:               "work aliases",
			args:               []string{"agent", "--address", "localhost:9090", "--poll_interval", "10s", "--report_interval", "10s", "--timeout", "10s"},
			wantPollInterval:   10 * time.Second,
			wantReportInterval: 10 * time.Second,
			wantTimeout:        10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KEY", "")

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
			},
			wantAddress:        "localhost:9090",
			wantPollInterval:   5 * time.Second,
			wantReportInterval: 15 * time.Second,
			wantTimeout:        20 * time.Second,
			wantKey:            "env-secret",
		},
		{
			name: "flag overrides env",
			args: []string{"agent", "-a", "localhost:7777", "-p", "3", "-r", "7s", "-t", "10s", "-k", "flag-secret"},
			env: map[string]string{
				"ADDRESS":         "localhost:9090",
				"POLL_INTERVAL":   "5s",
				"REPORT_INTERVAL": "15s",
				"TIMEOUT":         "20s",
				"KEY":             "env-secret",
			},
			wantAddress:        "localhost:7777",
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
			wantTimeout:        10 * time.Second,
			wantKey:            "flag-secret",
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KEY", "")
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
