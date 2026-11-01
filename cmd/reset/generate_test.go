package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateProducesWorkingMethods(t *testing.T) {
	root := copyFixture(t)
	require.NoError(t, generate(root))
	for _, path := range []string{filepath.Join(root, "reset.gen.go"), filepath.Join(root, "child", "reset.gen.go")} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		require.NoError(t, err)
		require.True(t, ast.IsGenerated(file), "Go должен распознавать сгенерированный файл: %s", path)
	}

	runModuleTests(t, root)
}

func runModuleTests(t *testing.T, root string) {
	t.Helper()
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

func TestGenerateBeforeResetExists(t *testing.T) {
	root := newModule(t, map[string]string{
		"model.go": `package sample
import "example.com/resetfixture/child"
// generate:reset
type Value struct { Child child.Counter }
var _ interface { Reset() } = (*Value)(nil)
var _ interface { Reset() } = (*child.Counter)(nil)
func Clear(value *Value) { value.Reset() }
`,
		"child/model.go": "package child\n// generate:reset\ntype Counter struct { Count int }\n",
		"model_test.go": `package sample
import "testing"
func TestReset(t *testing.T) {
    value := new(Value)
    value.Child.Count = 9
    Clear(value)
    if value.Child.Count != 0 { t.Fatal("вложенная структура не сброшена") }
}
`,
	})
	require.NoError(t, generate(root))
	runModuleTests(t, root)
}

func TestGenerateAgain(t *testing.T) {
	root := copyFixture(t)
	require.NoError(t, generate(root))
	paths := []string{filepath.Join(root, "reset.gen.go"), filepath.Join(root, "child", "reset.gen.go")}
	before := make([][]byte, len(paths))
	for i, path := range paths {
		var err error
		before[i], err = os.ReadFile(path)
		require.NoError(t, err)
	}
	require.NoError(t, generate(root))
	for i, path := range paths {
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, before[i], after)
	}
}

func TestGenerateUpdatesChangedTypes(t *testing.T) {
	root := newModule(t, map[string]string{
		"model.go": `package sample
// generate:reset
type Value struct { Old string }
// generate:reset
type Gone struct { Count int }
// generate:reset
type Unmarked struct { Count int }
`,
	})
	require.NoError(t, generate(root))
	updated := `package sample
// generate:reset
type Value struct { New int; Items []string }
type Unmarked struct { Count int }
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "model.go"), []byte(updated), 0o644))
	test := `package sample
import "testing"
func TestReset(t *testing.T) {
    value := Value{New: 5, Items: []string{"value"}}
    value.Reset()
    if value.New != 0 || len(value.Items) != 0 { t.Fatal("новые поля не сброшены") }
    if _, ok := any(&Unmarked{}).(interface { Reset() }); ok { t.Fatal("лишний Reset") }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "model_test.go"), []byte(test), 0o644))
	require.NoError(t, generate(root))
	runModuleTests(t, root)

	require.NoError(t, os.WriteFile(filepath.Join(root, "model.go"), []byte("package sample\ntype Value struct { New int }\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(root, "model_test.go")))
	require.NoError(t, generate(root))
	require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
	runModuleTests(t, root)
}

func TestGenerateRejectsResetConflicts(t *testing.T) {
	for _, tt := range []struct {
		name string
		code string
	}{
		{name: "manual method", code: "// generate:reset\ntype Value struct{}\nfunc (*Value) Reset() {}"},
		{name: "field", code: "// generate:reset\ntype Value struct{ Reset int }"},
		{name: "arguments", code: "type Child struct{}\nfunc (*Child) Reset(int) {}\n// generate:reset\ntype Value struct{ Child Child }"},
		{name: "result", code: "type Child struct{}\nfunc (*Child) Reset() int { return 1 }\n// generate:reset\ntype Value struct{ Child Child }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := newModule(t, map[string]string{"model.go": "package sample\n" + tt.code + "\n"})
			require.ErrorContains(t, generate(root), "Reset")
			require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
		})
	}
}

func TestGenerateRejectsRecursiveTypes(t *testing.T) {
	if os.Getenv("RESET_TYPE_CYCLE_CHILD") == "1" {
		// Ограничиваем стек отдельного процесса, чтобы ошибка не уронила все тесты.
		debug.SetMaxStack(256 << 10)
		for _, declaration := range []string{"type Recursive *Recursive", "type Recursive [1]*Recursive"} {
			root := newModule(t, map[string]string{
				"model.go": "package sample\n" + declaration + "\n// generate:reset\ntype Value struct { Next Recursive }\n",
			})
			require.ErrorContains(t, generate(root), "рекурсивный тип")
			require.NoFileExists(t, filepath.Join(root, "reset.gen.go"))
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGenerateRejectsRecursiveTypes$")
	command.Env = append(os.Environ(), "RESET_TYPE_CYCLE_CHILD=1")
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err())
	require.NoError(t, err, "%s", output)
}
