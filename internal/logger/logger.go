package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Фабрика с двумя педнастроенными конфиуграциями
func New(debug bool) (*zap.Logger, error) {
	if debug {
		return newDebugConfig().Build()
	}
	return newProductionConfig().Build()
}

func newProductionConfig() zap.Config {
	return zap.Config{
		Level: zap.NewAtomicLevelAt(zapcore.InfoLevel),
		// На проме всегда будет коллектор логов, а не терминал
		Encoding: "json",
		// Строгое перенаправление в stdout
		OutputPaths: []string{os.Stdout.Name()},
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:       "time",
			LevelKey:      "level",
			NameKey:       "logger",
			MessageKey:    "msg",
			StacktraceKey: "stacktrace",
			// Чисто взял параметры по рекомендации 12-factor app
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
	}
}

func newDebugConfig() zap.Config {
	// Для переопределения базовых параметров
	// Плюс меньше дублирования кода
	cfg := newProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	// json нам читать уже сложнее, так что выводим цветное и в консоль
	cfg.Encoding = "console"
	cfg.Development = true
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return cfg
}
