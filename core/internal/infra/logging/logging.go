// Package logging builds the process slog.Logger (text or JSON).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func New(w io.Writer, format, level string) (*slog.Logger, error) {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "", "info":
		lv = slog.LevelInfo
	case "debug":
		lv = slog.LevelDebug
	case "warn", "warning":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		return nil, fmt.Errorf("log level %q: want debug|info|warn|error", level)
	}
	opts := &slog.HandlerOptions{Level: lv}
	switch strings.ToLower(format) {
	case "", "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	}
	return nil, fmt.Errorf("log format %q: want text|json", format)
}
