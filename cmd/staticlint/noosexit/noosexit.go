package noosexit

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var Analyzer = &analysis.Analyzer{
	Name: "noosexit",
	Doc:  "запрещает прямой вызов os.Exit в функции main пакета main",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Name() != "main" {
		// Пропускаем если не main
		return nil, nil
	}

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "main" || fn.Recv != nil || fn.Body == nil {
				// Если не main
				continue
			}

			ast.Inspect(fn.Body, func(node ast.Node) bool {
				// У вложенной функции своё тело, оно не относится к main
				if _, ok := node.(*ast.FuncLit); ok {
					return false
				}

				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}

				// Смотрим на саму функцию, а не на имя импорта в коде.
				callee := typeutil.StaticCallee(pass.TypesInfo, call)
				if callee != nil && callee.Pkg() != nil &&
					callee.Pkg().Path() == "os" && callee.Name() == "Exit" {
					pass.Reportf(call.Pos(), "прямой вызов os.Exit запрещён в функции main пакета main")
				}

				return true
			})
		}
	}

	return nil, nil
}
