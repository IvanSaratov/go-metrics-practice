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

// ReadStringJSON читает необязательное строковое поле, сохраняя проверку его типа.
// В altsrc пустой массив превращается в пустую строку и может незаметно отключить настройку.
func ReadStringJSON(path, key string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config %q: %w", path, err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("decode config %q: %w", path, err)
	}
	value, exists := values[key]
	if !exists {
		return "", nil
	}
	var result *string
	if err := json.Unmarshal(value, &result); err != nil || result == nil {
		return "", fmt.Errorf("invalid config %q: %s must be a string", path, key)
	}
	return *result, nil
}
