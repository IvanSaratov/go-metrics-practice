package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadPackagesFindsMarkedStructs(t *testing.T) {
	root := copyFixture(t)
	pkgs, err := loadPackages(root)
	require.NoError(t, err)

	found := make(map[string][]string)
	for _, pkg := range pkgs {
		for _, named := range pkg.structs {
			found[pkg.name] = append(found[pkg.name], named.Obj().Name())
		}
	}
	require.Equal(t, map[string][]string{
		"fixture": {"Basic", "Empty", "Grouped", "Second"},
		"child":   {"Counter"},
	}, found)
}

func TestLoadPackagesWithoutMarkers(t *testing.T) {
	root := newModule(t, map[string]string{
		"model.go": "package sample\ntype Model struct { Value int }\n",
	})
	pkgs, err := loadPackages(root)
	require.NoError(t, err)
	for _, pkg := range pkgs {
		require.Empty(t, pkg.structs)
	}
}

func TestLoadPackagesRejectsInvalidMarkers(t *testing.T) {
	for _, declaration := range []string{
		"type Value int",
		"type Value = struct { Count int }",
		"type Value[T any] struct { Count int }",
	} {
		t.Run(declaration, func(t *testing.T) {
			root := newModule(t, map[string]string{
				"model.go": "package sample\n// generate:reset\n" + declaration + "\n",
			})
			_, err := loadPackages(root)
			require.ErrorContains(t, err, "Value")
		})
	}
}

func copyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS("testdata")))
	return root
}

func newModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/resetfixture\n\ngo 1.26.4\n"), 0o644))
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}
