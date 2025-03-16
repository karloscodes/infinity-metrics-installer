package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config holds logger configuration
type Config struct {
	Level    string // "debug", "info", "warn", "error"
	NoColor  bool   // Ignored with Zap (handled by encoder)
	Verbose  bool   // Maps to debug level
	ShowTime bool   // Always true with Zap
	LogDir   string // Directory for log files (e.g., "/opt/infinity-metrics/logs")
}

// Logger provides structured logging functionality
type Logger struct {
	zapLogger *zap.Logger
}

// NewLogger creates a new logger instance with Zap and Lumberjack
func NewLogger(config Config) *Logger {
	// Map config.Level to Zap levels
	var level zapcore.Level
	switch strings.ToLower(config.Level) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}
	if config.Verbose {
		level = zapcore.DebugLevel
	}

	// Console encoder (human-readable, with colors)
	consoleEncoder := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // Colored levels
		EncodeTime:     zapcore.ISO8601TimeEncoder,       // e.g., "2025-03-16T12:00:00Z"
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	// File encoder (JSON, for structured logging)
	fileEncoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	// Log directory and file
	logDir := config.LogDir
	if logDir == "" {
		logDir = "/opt/infinity-metrics/logs" // Default, adjust if needed
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create log directory %s: %v\n", logDir, err)
	}
	logFile := filepath.Join(logDir, "infinity-metrics.log")

	// Lumberjack for log rotation
	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    10, // MB
		MaxBackups: 3,  // Number of backup files
		MaxAge:     28, // Days
		Compress:   true,
	}

	// Core setup: write to both stdout and file
	consoleCore := zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), level)
	fileCore := zapcore.NewCore(fileEncoder, zapcore.Lock(zapcore.AddSync(lumberjackLogger)), level)
	core := zapcore.NewTee(consoleCore, fileCore)

	// Create logger
	zapLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	return &Logger{zapLogger: zapLogger}
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.zapLogger.Debug(fmt.Sprintf(format, args...))
}

// Info logs an informational message
func (l *Logger) Info(format string, args ...interface{}) {
	l.zapLogger.Info(fmt.Sprintf(format, args...))
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.zapLogger.Warn(fmt.Sprintf(format, args...))
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.zapLogger.Error(fmt.Sprintf(format, args...))
}

// Success logs a success message (mapped to Info level with custom field)
func (l *Logger) Success(format string, args ...interface{}) {
	l.zapLogger.Info(fmt.Sprintf(format, args...), zap.String("status", "success"))
}

// Step logs an installation step (mapped to Info level with step fields)
func (l *Logger) Step(step int, total int, format string, args ...interface{}) {
	l.zapLogger.Info(fmt.Sprintf(format, args...), zap.Int("step", step), zap.Int("total", total))
}

// Progress updates installation progress (mapped to Info level with percent field)
func (l *Logger) Progress(percent int, format string, args ...interface{}) {
	l.zapLogger.Info(fmt.Sprintf(format, args...), zap.Int("progress", percent))
}

// Sync flushes any buffered logs (call on shutdown if needed)
func (l *Logger) Sync() {
	l.zapLogger.Sync()
}
