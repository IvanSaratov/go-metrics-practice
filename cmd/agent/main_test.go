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
	var got agentConfig
	app := newAgentApp(func(config agentConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"agent", "-a", "localhost:9090", "-p", "3", "-r", "7"})

	require.NoError(t, err)
	require.Equal(t, "localhost:9090", got.serverAddress)
	require.Equal(t, 3*time.Second, got.pollInterval)
	require.Equal(t, 7*time.Second, got.reportInterval)
}

func TestAgentAppRejectsUnknownFlag(t *testing.T) {
	app := newAgentApp(func(config agentConfig) error {
		return nil
	})

	err := app.Run([]string{"agent", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}
