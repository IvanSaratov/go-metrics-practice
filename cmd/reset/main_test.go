package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommand(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "reset")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "%s", output)

	for _, tt := range []struct {
		name string
		decl string
		code int
	}{
		{name: "success", decl: "type Value struct { Count int }", code: 0},
		{name: "invalid marker", decl: "type Value int", code: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := newModule(t, map[string]string{
				"model.go": "package sample\n// generate:reset\n" + tt.decl + "\n",
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, binary)
			command.Dir = root
			output, err := command.CombinedOutput()
			require.NoError(t, ctx.Err())
			if tt.code == 0 {
				require.NoError(t, err, "%s", output)
				require.FileExists(t, filepath.Join(root, "reset.gen.go"))
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				require.Equal(t, tt.code, exitErr.ExitCode())
				require.Contains(t, string(output), "Value")
				require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
			}
		})
	}
}
