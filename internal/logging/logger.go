// internal/logging/logger.go
package logging

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Log levels
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Config holds logger configuration
type Config struct {
	Level    string
	NoColor  bool
	Verbose  bool
	ShowTime bool
}

// Logger provides structured logging functionality
type Logger struct {
	level    int
	noColor  bool
	verbose  bool
	showTime bool
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
)

// NewLogger creates a new logger instance
func NewLogger(config Config) *Logger {
	level := LevelInfo
	switch strings.ToLower(config.Level) {
	case "debug":
		level = LevelDebug
	case "info":
		level = LevelInfo
	case "warn":
		level = LevelWarn
	case "error":
		level = LevelError
	}

	return &Logger{
		level:    level,
		noColor:  config.NoColor,
		verbose:  config.Verbose,
		showTime: config.ShowTime,
	}
}

// formatMessage formats a log message with optional timestamp
func (l *Logger) formatMessage(level, color, prefix string, format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)

	var timeStr string
	if l.showTime {
		timeStr = time.Now().Format("15:04:05") + " "
	}

	if l.noColor {
		return fmt.Sprintf("%s[%s] %s%s", timeStr, level, prefix, msg)
	}

	return fmt.Sprintf("%s%s[%s]%s %s%s%s", timeStr, color, level, colorReset, prefix, msg, colorReset)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.level <= LevelDebug {
		fmt.Fprintln(os.Stdout, l.formatMessage("DEBUG", colorBlue, "", format, args...))
	}
}

// Info logs an informational message
func (l *Logger) Info(format string, args ...interface{}) {
	if l.level <= LevelInfo {
		fmt.Fprintln(os.Stdout, l.formatMessage("INFO", colorCyan, "", format, args...))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	if l.level <= LevelWarn {
		fmt.Fprintln(os.Stderr, l.formatMessage("WARN", colorYellow, "", format, args...))
	}
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	if l.level <= LevelError {
		fmt.Fprintln(os.Stderr, l.formatMessage("ERROR", colorRed, "", format, args...))
	}
}

// Success logs a success message (always shown)
func (l *Logger) Success(format string, args ...interface{}) {
	fmt.Fprintln(os.Stdout, l.formatMessage("SUCCESS", colorGreen, "", format, args...))
}

// Step logs an installation step
func (l *Logger) Step(step int, total int, format string, args ...interface{}) {
	if l.level <= LevelInfo {
		prefix := fmt.Sprintf("[%d/%d] ", step, total)
		fmt.Fprintln(os.Stdout, l.formatMessage("STEP", colorPurple, prefix, format, args...))
	}
}

// Progress updates installation progress
func (l *Logger) Progress(percent int, format string, args ...interface{}) {
	if l.level <= LevelInfo {
		prefix := fmt.Sprintf("[%d%%] ", percent)
		fmt.Fprintln(os.Stdout, l.formatMessage("PROGRESS", colorPurple, prefix, format, args...))
	}
}
