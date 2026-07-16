package logger

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNewProductionLoggerEmitsInfoNotDebug(t *testing.T) {
	logger, err := New(false)
	require.NoError(t, err)

	require.False(t, logger.Core().Enabled(zapcore.DebugLevel), "в production-режиме debug-сообщения должны отсекаться")
	require.True(t, logger.Core().Enabled(zapcore.InfoLevel), "в production-режиме info-сообщения должны проходить")
}

func TestNewDebugLoggerEmitsDebug(t *testing.T) {
	logger, err := New(true)
	require.NoError(t, err)

	require.True(t, logger.Core().Enabled(zapcore.DebugLevel), "в debug-режиме debug-сообщения должны проходить")
	require.True(t, logger.Core().Enabled(zapcore.InfoLevel), "в debug-режиме info-сообщения должны проходить")
}

// Проверяем, что production-конфиг реально выдаёт JSON с нужными ключами и отсекает debug-сообщения
func TestProductionConfigProducesJSONOutput(t *testing.T) {
	var buf bytes.Buffer

	cfg := newProductionConfig()
	encoder := zapcore.NewJSONEncoder(cfg.EncoderConfig)
	core := zapcore.NewCore(encoder, zapcore.AddSync(&buf), cfg.Level)
	logger := zap.New(core)

	logger.Info("incoming request", zap.String("method", "GET"), zap.String("uri", "/"))
	logger.Debug("should not appear")
	require.NoError(t, logger.Sync())

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	// Проверяем обязательные ключи production-формата.
	require.Equal(t, "info", entry["level"])
	require.Equal(t, "incoming request", entry["msg"])
	require.Contains(t, entry, "time")

	// Структурированные поля должны попасть в JSON.
	require.Equal(t, "GET", entry["method"])
	require.Equal(t, "/", entry["uri"])

	// Debug-сообщение не должно попасть в вывод.
	require.NotContains(t, buf.String(), "should not appear", "debug-сообщение не должно попадать в production-вывод")
}
