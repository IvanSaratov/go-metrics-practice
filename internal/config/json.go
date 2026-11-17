// Package config проверяет конфигурацию агента и сервера.
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ValidateJSON проверяет файл конфигурации. Пустой путь означает запуск без файла.
func ValidateJSON(path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("config %q contains invalid JSON", path)
	}

	// JSON-источник altsrc использует YAML-парсер. Проверяем, что он примет объект.
	var config map[any]any
	if err := yaml.Unmarshal(data, &config); err != nil || config == nil {
		return fmt.Errorf("config %q must contain a JSON object without duplicate keys", path)
	}
	return nil
}
