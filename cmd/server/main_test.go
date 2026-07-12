package main

import (
	"testing"

	"github.com/stretchr/testify/require"
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
