package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentAppUsesDefaults(t *testing.T) {
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
}

func TestAgentAppParsesFlags(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		wantPollInterval   time.Duration
		wantReportInterval time.Duration
	}{
		{
			name:               "numeric poll interval and duration report interval",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "3", "-r", "7s"},
			wantPollInterval:   3 * time.Second,
			wantReportInterval: 7 * time.Second,
		},
		{
			name:               "hour and minute intervals",
			args:               []string{"agent", "-a", "localhost:9090", "-p", "1h", "-r", "2m"},
			wantPollInterval:   time.Hour,
			wantReportInterval: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
