package observability

import (
	"log/slog"
	"os"
)

// NewLogger returns a JSON structured logger. No secrets are ever logged by callers.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
