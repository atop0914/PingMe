package logger

import (
	"log/slog"
	"os"
	"sync"
)

var (
	log     *slog.Logger
	logOnce sync.Once
)

// Init initializes the global logger
func Init(level string, format string, output string) {
	logOnce.Do(func() {
		var logLevel slog.Level
		switch level {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			logLevel = slog.LevelInfo
		}

		var handler slog.Handler
		if format == "json" {
			handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: logLevel,
			})
		} else {
			handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: logLevel,
			})
		}

		log = slog.New(handler)
		slog.SetDefault(log)
	})
}

// Get returns the global logger instance
func Get() *slog.Logger {
	if log == nil {
		// Default initialization
		Init("info", "json", "stdout")
	}
	return log
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	Get().Debug(msg, args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	Get().Info(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	Get().Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	Get().Error(msg, args...)
}

// With returns a new logger with the given attributes
func With(args ...any) *slog.Logger {
	return Get().With(args...)
}
