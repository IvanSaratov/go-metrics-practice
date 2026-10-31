package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateProducesWorkingMethods(t *testing.T) {
	root := copyFixture(t)
	require.NoError(t, generate(root))
	require.FileExists(t, filepath.Join(root, "reset.gen.go"))
	require.FileExists(t, filepath.Join(root, "child", "reset.gen.go"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "%s", output)
}

func TestGenerateWithoutMarkers(t *testing.T) {
	root := newModule(t, map[string]string{
		"model.go": "package sample\ntype Value struct { Count int }\n",
	})
	require.NoError(t, generate(root))
	require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
}

func TestGeneratePreservesForeignFile(t *testing.T) {
	foreign := "package child\n// Этот файл написан вручную.\n"
	root := newModule(t, map[string]string{
		"model.go":           "package sample\n// generate:reset\ntype Value struct { Count int }\n",
		"child/model.go":     "package child\n// generate:reset\ntype Child struct { Count int }\n",
		"child/reset.gen.go": foreign,
	})
	err := generate(root)
	require.ErrorContains(t, err, "reset.gen.go")
	content, err := os.ReadFile(filepath.Join(root, "child", "reset.gen.go"))
	require.NoError(t, err)
	require.Equal(t, foreign, string(content))
	require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
}

func TestGenerateDoesNotWriteAfterLoadError(t *testing.T) {
	root := newModule(t, map[string]string{
		"model.go":       "package sample\n// generate:reset\ntype Value struct { Count int }\n",
		"child/model.go": "package child\n// generate:reset\ntype Wrong int\n",
	})
	require.Error(t, generate(root))
	require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
	require.NoFileExists(t, filepath.Join(root, "child", "reset.gen.go"))
}
