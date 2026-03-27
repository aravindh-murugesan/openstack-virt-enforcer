package workflow

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/lmittmann/tint"
)

// SetupLogger configures a structured logger template for uniformity across workflows.
//
// It uses the [tint] handler to provide colorized output for better terminal
// readability. The logger is automatically contextualized with the
// "cloud_profile" attribute to assist in multi-cloud log filtering.
func SetupLogger(level string, cloudName string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level: logLevel,
	})

	return slog.New(handler).With("cloud_profile", cloudName)
}

func (l *Logger) SetupLogger() {

	if l.Level == "" {
		l.Level = "info"
	}

	var logLevel slog.Level
	switch l.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	if l.Instance == nil {
		handler := tint.NewHandler(os.Stderr, &tint.Options{
			Level:   logLevel,
			NoColor: false,
		})
		l.Instance = slog.New(handler)
	}

	if l.RunID == "" {
		l.RunID = fmt.Sprintf("ve-%s", uuid.NewString())
	}
}
