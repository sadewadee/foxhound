package foxhound

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// SetupLogging configures the global slog logger from a LoggingConfig.
// The verbose parameter overrides the config level:
//
//	0 = use config level (default info)
//	1 = debug  (-v)
//	2 = debug with source location (-vv)
func SetupLogging(cfg LoggingConfig, verbose int) {
	level := parseLogLevel(cfg.Level)
	if verbose >= 1 {
		level = slog.LevelDebug
	}

	output := logOutput(cfg.Output)
	addSource := verbose >= 2

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: addSource,
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(output, opts)
	default:
		handler = slog.NewJSONHandler(output, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// parseLogLevel converts a string level to slog.Level.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// logOutput returns the io.Writer for the configured output target.
func logOutput(s string) io.Writer {
	switch strings.ToLower(s) {
	case "stdout":
		return os.Stdout
	default:
		return os.Stderr
	}
}
