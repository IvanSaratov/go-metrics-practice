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
		path := filepath.Join(pkg.dir, "reset.gen.go")
		previous, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("чтение %s: %w", path, err)
		}
		owned := err == nil && bytes.HasPrefix(previous, []byte(generatedHeader))
		if len(pkg.structs) == 0 {
			if owned {
				// Меток больше нет, поэтому свой старый файл нужно удалить.
				files = append(files, outputFile{path: path})
			}
			continue
		}
		// По первой строке узнаём свой файл, чтобы не затереть чужой
		if err == nil && !owned {
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
		// nil в data означает, что файл нужно удалить.
		if file.data == nil {
			if err := os.Remove(file.path); err != nil {
				return fmt.Errorf("удаление %s: %w", file.path, err)
			}
			continue
		}
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
			code, err := resetValue(expr, field.Type(), nil)
			if err != nil {
				return nil, fmt.Errorf("поле %s.%s: %w", named.Obj().Name(), field.Name(), err)
			}
			out.WriteString(code)
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

// resetValue выбирает, какой код нужен для генерации Reset.
func resetValue(expr string, fieldType types.Type, parents []types.Type) (string, error) {
	// Повтор типа при разборе указателей или массивов зациклит генератор.
	for _, parent := range parents {
		if types.Identical(parent, fieldType) {
			return "", fmt.Errorf("рекурсивный тип %s пока не поддерживается", fieldType)
		}
	}
	// Раскрываем именованные типы: например, type count int тоже сбрасываем в 0.
	switch typ := fieldType.Underlying().(type) {
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
		return fmt.Sprintf("%s = %s\n", expr, zero), nil
	case *types.Slice:
		// Оставляем память среза для повторного использования.
		return fmt.Sprintf("%s = %s[:0]\n", expr, expr), nil
	case *types.Map:
		// Очищаем существующую мапу, не создавая новую.
		return fmt.Sprintf("clear(%s)\n", expr), nil
	case *types.Pointer:
		// Применяем те же правила к значению за указателем, сам указатель сохраняем.
		code, err := resetValue("(*"+expr+")", typ.Elem(), append(parents, fieldType))
		if err != nil || code == "" {
			return code, err
		}
		// К значению обращаемся только после проверки указателя на nil.
		return fmt.Sprintf("if %s != nil {\n%s}\n", expr, code), nil
	case *types.Struct:
		// Ищем Reset, в том числе метод с указательным получателем.
		object, _, _ := types.LookupFieldOrMethod(fieldType, true, nil, "Reset")
		method, ok := object.(*types.Func)
		if !ok {
			// Структуру без Reset оставляем как есть.
			return "", nil
		}
		signature := method.Type().(*types.Signature)
		// Поддерживаем только Reset() без аргументов и результата.
		if signature.Params().Len() != 0 || signature.Results().Len() != 0 {
			return "", fmt.Errorf("метод %s.Reset должен быть без параметров и результатов", fieldType)
		}
		return fmt.Sprintf("%s.Reset()\n", expr), nil
	case *types.Array:
		// Для вложенных массивов используем разные индексы: i0, i1 и так дальше.
		index := fmt.Sprintf("i%d", len(parents))
		code, err := resetValue(expr+"["+index+"]", typ.Elem(), append(parents, fieldType))
		if err != nil || code == "" {
			return code, err
		}
		return fmt.Sprintf("for %s := range %s {\n%s}\n", index, expr, code), nil
	case *types.Chan, *types.Signature, *types.Interface:
		// У этих типов нулевое значение.
		return fmt.Sprintf("%s = nil\n", expr), nil
	default:
		return "", fmt.Errorf("тип %s пока не поддерживается", fieldType)
	}
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
