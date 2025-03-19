package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level   string
	Verbose bool
	LogDir  string
	Quiet   bool
}

type Logger struct {
	*logrus.Logger
	fileLogging bool
}

func NewLogger(config Config) *Logger {
	logger := logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
		DisableColors:    false,
		DisableQuote:     true,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyLevel: "",
			logrus.FieldKeyMsg:   "",
		},
	})

	switch config.Level {
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "info":
		logger.SetLevel(logrus.InfoLevel)
	case "warn":
		logger.SetLevel(logrus.WarnLevel)
	case "error":
		logger.SetLevel(logrus.ErrorLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}
	if config.Verbose {
		logger.SetLevel(logrus.DebugLevel)
	}
	if config.Quiet {
		logger.SetLevel(logrus.ErrorLevel)
	}

	return &Logger{
		Logger:      logger,
		fileLogging: false,
	}
}

func NewFileLogger(config Config) *Logger {
	logger := NewLogger(config)
	logger.fileLogging = true

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
	logFile := filepath.Join(logDir, "infinity-metrics-cli.log")

	logger.AddHook(&FileHook{
		Writer: &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    10,
			MaxBackups: 3,
			MaxAge:     28,
			Compress:   true,
		},
		Formatter: &logrus.JSONFormatter{
			TimestampFormat: "15:04:05",
		},
	})

	return logger
}

type FileHook struct {
	Writer    io.Writer
	Formatter logrus.Formatter
}

func (h *FileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *FileHook) Fire(entry *logrus.Entry) error {
	line, err := h.Formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = h.Writer.Write(line)
	return err
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.Logger.Debug(fmt.Sprintf(format, args...))
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.Logger.Info(fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.Logger.Warn(fmt.Sprintf(format, args...))
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.Logger.Error(fmt.Sprintf(format, args...))
}

func (l *Logger) Success(format string, args ...interface{}) {
	msg := fmt.Sprintf("✔ "+format, args...)
	l.Logger.Info(msg)
	if l.fileLogging {
		l.Logger.WithField("status", "success").Info(fmt.Sprintf(format, args...))
	}
}

func (l *Logger) Step(step, total int, format string, args ...interface{}) {
	msg := fmt.Sprintf("➜ Step %d/%d: %s", step, total, fmt.Sprintf(format, args...))
	l.Logger.Info(msg)
	if l.fileLogging {
		l.Logger.WithFields(logrus.Fields{
			"step":  step,
			"total": total,
		}).Info(fmt.Sprintf(format, args...))
	}
}

func (l *Logger) InfoWithTime(format string, args ...interface{}) {
	msg := fmt.Sprintf("%s [INFO ] %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	l.Logger.Info(msg)
}

func (l *Logger) StartSpinner(format string, args ...interface{}) chan struct{} {
	stop := make(chan struct{})
	go func() {
		spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-stop:
				fmt.Fprintf(os.Stdout, "\r%-80s\r", "") // Clear line
				os.Stdout.Sync()                        // Force flush
				return
			default:
				fmt.Fprintf(os.Stdout, "\r%s %s", spinner[i%len(spinner)], fmt.Sprintf(format, args...))
				os.Stdout.Sync()
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return stop
}

func (l *Logger) StopSpinner(stop chan struct{}, success bool, format string, args ...interface{}) {
	close(stop)
	time.Sleep(200 * time.Millisecond) // Increased delay for test environments
	if success {
		l.Success(format, args...)
	} else {
		l.Error(format, args...)
	}
	l.Logger.Out.(*os.File).Sync() // Ensure all logs are flushed
}

func DefaultConfig() Config {
	return Config{
		Level:   "info",
		Verbose: false,
		LogDir:  "",
		Quiet:   false,
	}
}
