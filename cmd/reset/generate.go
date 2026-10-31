package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/types"
	"os"
	"path/filepath"
)

const generatedHeader = "// Сгенерировано командой go run ./cmd/reset. Не редактировать.\n"

func generate(root string) error {
	pkgs, err := loadPackages(root)
	if err != nil {
		return err
	}

	type outputFile struct {
		path string
		data []byte
	}
	var files []outputFile
	for _, pkg := range pkgs {
		if len(pkg.structs) == 0 {
			continue
		}
		path := filepath.Join(pkg.dir, "reset.gen.go")
		previous, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("чтение %s: %w", path, err)
		}
		// По первой строке узнаём свой файл, чтобы не затереть чужой
		if err == nil && !bytes.HasPrefix(previous, []byte(generatedHeader)) {
			return fmt.Errorf("файл %s создан не этой утилитой", path)
		}
		data, err := renderPackage(pkg)
		if err != nil {
			return err
		}
		files = append(files, outputFile{path: path, data: data})
	}

	// Сначала проверяем все пакеты, потом записываем результат
	for _, file := range files {
		if err := writeGeneratedFile(file.path, file.data); err != nil {
			return fmt.Errorf("запись %s: %w", file.path, err)
		}
	}
	return nil
}

func renderPackage(pkg packageInfo) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString(generatedHeader)
	fmt.Fprintf(&out, "\npackage %s\n", pkg.name)
	for _, named := range pkg.structs {
		fmt.Fprintf(&out, "\nfunc (r *%s) Reset() {\nif r == nil { return }\n", named.Obj().Name())
		structure := named.Underlying().(*types.Struct)
		for field := range structure.Fields() {
			if field.Name() == "_" {
				continue
			}
			expr := "r." + field.Name()
			// Раскрываем именованные типы: например, type count int тоже сбрасываем в 0
			switch typ := field.Type().Underlying().(type) {
			case *types.Basic:
				zero := "0"
				switch {
				case typ.Info()&types.IsBoolean != 0:
					zero = "false"
				case typ.Info()&types.IsString != 0:
					zero = `""`
				case typ.Kind() == types.UnsafePointer:
					zero = "nil"
				}
				fmt.Fprintf(&out, "%s = %s\n", expr, zero)
			case *types.Slice:
				// Оставляем память среза для повторного использования
				fmt.Fprintf(&out, "%s = %s[:0]\n", expr, expr)
			case *types.Map:
				// Очищаем существующую мапу, не создавая новую
				fmt.Fprintf(&out, "clear(%s)\n", expr)
			default:
				return nil, fmt.Errorf("поле %s.%s: тип %s пока не поддерживается", named.Obj().Name(), field.Name(), field.Type())
			}
		}
		out.WriteString("}\n")
	}
	// Проверяем синтаксис и форматируем полученный Go-код
	data, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("форматирование пакета %s: %w", pkg.name, err)
	}
	return data, nil
}

func writeGeneratedFile(path string, data []byte) error {
	// Пишем во временный файл, чтобы при ошибке записи старый остался целым.
	file, err := os.CreateTemp(filepath.Dir(path), ".reset-*.go")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if err := file.Chmod(0o644); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
