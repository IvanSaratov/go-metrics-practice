package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type packageInfo struct {
	dir     string
	name    string
	structs []*types.Named
}

func loadPackages(root string) ([]packageInfo, error) {
	// Загружаем исходники и типы всех пакетов. Тестовые файлы сюда не входят.
	pkgs, err := packages.Load(&packages.Config{
		Dir:  root,
		Mode: packages.LoadSyntax,
	}, "./...")
	if err != nil {
		return nil, fmt.Errorf("загрузка пакетов: %w", err)
	}

	var result []packageInfo
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("пакет %s: %s", pkg.PkgPath, pkg.Errors[0])
		}
		if len(pkg.GoFiles) == 0 {
			continue
		}
		info := packageInfo{dir: filepath.Dir(pkg.GoFiles[0]), name: pkg.Name}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok || group.Tok != token.TYPE {
					continue
				}
				for _, spec := range group.Specs {
					typeSpec := spec.(*ast.TypeSpec)
					// Метка может стоять над type или над структурой внутри type (...).
					if !hasResetMarker(group.Doc) && !hasResetMarker(typeSpec.Doc) {
						continue
					}
					if _, ok := typeSpec.Type.(*ast.StructType); !ok || typeSpec.Assign.IsValid() {
						return nil, fmt.Errorf("%s: метка generate:reset допустима только у определения структуры %s", pkg.Fset.Position(typeSpec.Pos()), typeSpec.Name.Name)
					}
					if typeSpec.TypeParams != nil {
						return nil, fmt.Errorf("%s: обобщённая структура %s пока не поддерживается", pkg.Fset.Position(typeSpec.Pos()), typeSpec.Name.Name)
					}
					// Сохраняем тип структуры, чтобы потом разобрать её поля.
					named := pkg.TypesInfo.Defs[typeSpec.Name].Type().(*types.Named)
					info.structs = append(info.structs, named)
				}
			}
		}
		// Порядок методов должен быть одинаковым при каждом запуске.
		sort.Slice(info.structs, func(i, j int) bool {
			return info.structs[i].Obj().Name() < info.structs[j].Obj().Name()
		})
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].dir < result[j].dir })
	return result, nil
}

func hasResetMarker(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if strings.TrimSpace(comment.Text) == "// generate:reset" {
			return true
		}
	}
	return false
}
