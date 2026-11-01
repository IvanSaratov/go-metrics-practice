package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
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
	// Сначала получаем список файлов без проверки типов
	pkgs, err := packages.Load(&packages.Config{
		Dir:  root,
		Mode: packages.NeedName | packages.NeedFiles,
	}, "./...")
	if err != nil {
		return nil, fmt.Errorf("загрузка пакетов: %w", err)
	}
	overlay, err := prepareOverlay(pkgs)
	if err != nil {
		return nil, err
	}
	// Теперь можно проверить типы, даже если Reset ещё не сгенерирован.
	pkgs, err = packages.Load(&packages.Config{
		Dir:     root,
		Mode:    packages.LoadSyntax,
		Overlay: overlay,
	}, "./...")
	if err != nil {
		return nil, fmt.Errorf("загрузка типов: %w", err)
	}

	var result []packageInfo
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("пакет %s: %s", pkg.PkgPath, pkg.Errors[0])
		}
		if len(pkg.GoFiles) == 0 {
			continue
		}
		structs, err := findMarkedStructs(pkg)
		if err != nil {
			return nil, err
		}
		result = append(result, packageInfo{
			dir:     filepath.Dir(pkg.GoFiles[0]),
			name:    pkg.Name,
			structs: structs,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].dir < result[j].dir })
	return result, nil
}

// prepareOverlay готовит копии исходников в памяти.
func prepareOverlay(pkgs []*packages.Package) (map[string][]byte, error) {
	overlay := make(map[string][]byte)
	fset := token.NewFileSet()
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("пакет %s: %s", pkg.PkgPath, pkg.Errors[0])
		}
		var methods bytes.Buffer
		var firstSource []byte
		for i, path := range pkg.GoFiles {
			source, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("чтение %s: %w", path, err)
			}
			if filepath.Base(path) == "reset.gen.go" && bytes.HasPrefix(source, []byte(generatedHeader)) {
				// Старые методы могут ссылаться на уже удалённые поля или типы.
				source = []byte("package " + pkg.Name + "\n")
				overlay[path] = source
			}
			if i == 0 {
				// Все временные методы добавим в первый файл пакета.
				firstSource = source
			}
			file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
			if err != nil {
				return nil, fmt.Errorf("разбор %s: %w", path, err)
			}
			for _, spec := range markedTypeSpecs(file) {
				if _, ok := spec.Type.(*ast.StructType); ok && !spec.Assign.IsValid() && spec.TypeParams == nil {
					fmt.Fprintf(&methods, "\nfunc (*%s) Reset() {}\n", spec.Name.Name)
				}
			}
		}
		if methods.Len() > 0 {
			// Подставляем пустые Reset только в памяти, чтобы проверить их вызовы до генерации.
			overlay[pkg.GoFiles[0]] = append(firstSource, methods.Bytes()...)
		}
	}
	return overlay, nil
}

func findMarkedStructs(pkg *packages.Package) ([]*types.Named, error) {
	var structs []*types.Named
	for _, file := range pkg.Syntax {
		for _, spec := range markedTypeSpecs(file) {
			if _, ok := spec.Type.(*ast.StructType); !ok || spec.Assign.IsValid() {
				return nil, fmt.Errorf("%s: метка generate:reset допустима только у определения структуры %s", pkg.Fset.Position(spec.Pos()), spec.Name.Name)
			}
			if spec.TypeParams != nil {
				return nil, fmt.Errorf("%s: обобщённая структура %s пока не поддерживается", pkg.Fset.Position(spec.Pos()), spec.Name.Name)
			}
			// Находим объявленный тип структуры, чтобы потом разобрать её поля.
			object := pkg.TypesInfo.Defs[spec.Name]
			named := object.Type().(*types.Named)
			structs = append(structs, named)
		}
	}
	// Порядок методов должен быть одинаковым при каждом запуске.
	sort.Slice(structs, func(i, j int) bool {
		return structs[i].Obj().Name() < structs[j].Obj().Name()
	})
	return structs, nil
}

func markedTypeSpecs(file *ast.File) []*ast.TypeSpec {
	var specs []*ast.TypeSpec
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.TYPE {
			continue
		}
		for _, spec := range group.Specs {
			typeSpec := spec.(*ast.TypeSpec)
			// Метка может стоять над type или над структурой внутри type (...).
			if hasResetMarker(group.Doc) || hasResetMarker(typeSpec.Doc) {
				specs = append(specs, typeSpec)
			}
		}
	}
	return specs
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
