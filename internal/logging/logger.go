package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level    string
	NoColor  bool
	Verbose  bool
	ShowTime bool
	LogDir   string
	Quiet    bool
}

type Logger struct {
	zapLogger *zap.Logger
}

func NewLogger(config Config) *Logger {
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

	// Console encoder: No JSON fields, simpler format
	consoleEncoder := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		TimeKey:      "time",
		LevelKey:     "level",
		MessageKey:   "msg",
		LineEnding:   zapcore.DefaultLineEnding,
		EncodeLevel:  customLevelEncoder(config.NoColor),
		EncodeTime:   zapcore.TimeEncoderOfLayout("15:04:05"),
		EncodeCaller: zapcore.ShortCallerEncoder,
		EncodeName:   zapcore.FullNameEncoder,
	})

	// File encoder: Keep JSON fields
	fileEncoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	logDir := config.LogDir
	if logDir == "" {
		logDir = os.Getenv("LOG_DIR")
		if logDir == "" {
			logDir = "/opt/infinity-metrics/logs"
		}
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create log directory %s: %v\n", logDir, err)
	}
	logFile := filepath.Join(logDir, "infinity-metrics.log")

	lumberjackLogger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	var consoleCore zapcore.Core
	if config.Quiet {
		consoleCore = zapcore.NewNopCore()
	} else {
		consoleCore = zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), level)
	}
	fileCore := zapcore.NewCore(fileEncoder, zapcore.Lock(zapcore.AddSync(lumberjackLogger)), level)
	core := zapcore.NewTee(consoleCore, fileCore)

	zapLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	return &Logger{zapLogger: zapLogger}
}

func customLevelEncoder(noColor bool) zapcore.LevelEncoder {
	if noColor {
		return func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(fmt.Sprintf("%-5s", level.String()))
		}
	}
	return func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		switch level {
		case zapcore.DebugLevel:
			enc.AppendString("\x1b[36m[DEBUG]\x1b[0m")
		case zapcore.InfoLevel:
			enc.AppendString("\x1b[34m[INFO ]\x1b[0m")
		case zapcore.WarnLevel:
			enc.AppendString("\x1b[33m[WARN ]\x1b[0m")
		case zapcore.ErrorLevel:
			enc.AppendString("\x1b[31m[ERROR]\x1b[0m")
		default:
			enc.AppendString(fmt.Sprintf("[%-5s]", level.String()))
		}
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.zapLogger.Debug(fmt.Sprintf(format, args...))
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.zapLogger.Info(fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.zapLogger.Warn(fmt.Sprintf(format, args...))
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.zapLogger.Error(fmt.Sprintf(format, args...))
	l.zapLogger.Sync()
}

func (l *Logger) Success(format string, args ...interface{}) {
	// Console: No JSON fields
	l.zapLogger.Info(fmt.Sprintf("✔ "+format, args...))
	// File: Include status field
	l.zapLogger.Info(fmt.Sprintf(format, args...), zap.String("status", "success"))
}

func (l *Logger) Step(step int, total int, format string, args ...interface{}) {
	// Console: No JSON fields
	l.zapLogger.Info(fmt.Sprintf("➜ Step %d/%d: %s", step, total, fmt.Sprintf(format, args...)))
	// File: Include step and total fields
	l.zapLogger.Info(fmt.Sprintf(format, args...), zap.Int("step", step), zap.Int("total", total))
}

func (l *Logger) Progress(percent int, format string, args ...interface{}) {
	bar := strings.Repeat("█", percent/10) + strings.Repeat(" ", 10-percent/10)
	msg := fmt.Sprintf("[%s] %3d%% %s", bar, percent, format)
	l.zapLogger.Info(fmt.Sprintf(msg, args...), zap.Int("progress", percent))
}

func (l *Logger) StartSpinner(format string, args ...interface{}) chan struct{} {
	stop := make(chan struct{})
	go func() {
		spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-stop:
				fmt.Printf("\r%-80s", "") // Clear line
				return
			default:
				fmt.Printf("\r%s %s", spinner[i%len(spinner)], fmt.Sprintf(format, args...))
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return stop
}

func (l *Logger) StopSpinner(stop chan struct{}, success bool, format string, args ...interface{}) {
	close(stop)
	time.Sleep(110 * time.Millisecond) // Ensure spinner clears
	if success {
		l.Success(format, args...)
	} else {
		l.Error(format, args...)
	}
}

func (l *Logger) Sync() {
	l.zapLogger.Sync()
}

func DefaultConfig() Config {
	return Config{
		Level:    "info",
		NoColor:  false,
		Verbose:  false,
		ShowTime: true,
		LogDir:   "",
		Quiet:    false,
	}
}
