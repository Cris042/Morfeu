package logger

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.Logger with additional helper methods
type Logger struct {
	*zap.Logger
}

// NewLogger creates a new structured logger with JSON output
func NewLogger(level string) *Logger {
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		zapLevel = zapcore.InfoLevel
	}

	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zapLevel),
		Development: false,
		Encoding:    "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "timestamp",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			FunctionKey:    zapcore.OmitKey,
			MessageKey:     "message",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.RFC3339TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	zapLogger, err := config.Build(
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
		zap.WrapCore(NovoCoreRedator),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	return &Logger{Logger: zapLogger}
}

// Info logs an info message with structured fields
func (l *Logger) Info(message string, fields ...zapcore.Field) {
	l.Logger.Info(message, fields...)
}

// ErrorMsg logs an error message with structured fields
func (l *Logger) ErrorMsg(message string, fields ...zapcore.Field) {
	l.Error(message, fields...)
}

// Warn logs a warning message with structured fields
func (l *Logger) Warn(message string, fields ...zapcore.Field) {
	l.Logger.Warn(message, fields...)
}

// Debug logs a debug message with structured fields
func (l *Logger) Debug(message string, fields ...zapcore.Field) {
	l.Logger.Debug(message, fields...)
}

// String returns a zap field for string values
func (l *Logger) String(key, value string) zapcore.Field {
	return zap.String(key, value)
}

// ErrorField returns a zap field for error values
func (l *Logger) ErrorField(err error) zapcore.Field {
	return zap.Error(err)
}

// Int returns a zap field for int values
func (l *Logger) Int(key string, value int) zapcore.Field {
	return zap.Int(key, value)
}

// Int64 returns a zap field for int64 values
func (l *Logger) Int64(key string, value int64) zapcore.Field {
	return zap.Int64(key, value)
}

// Bool returns a zap field for bool values
func (l *Logger) Bool(key string, value bool) zapcore.Field {
	return zap.Bool(key, value)
}
