// Package logging builds the gateway's structured JSON log handler
// (req42.adoc F-23, GitHub issue #28): every log line is JSON with
// mandatory fields timestamp/level/component/trace_id, produced by
// wrapping slog's own JSON handler rather than replacing it outright — see
// NewHandler.
package logging

import (
	"fmt"
	"log/slog"
	"strings"
)

// ParseLevel maps a UDAL_LOG_LEVEL string ("debug"|"info"|"warn"|"error",
// case-insensitive; "" defaults to info) to a slog.Level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging: invalid level %q (want debug, info, warn, or error)", s)
	}
}

// NewLevelVar returns a *slog.LevelVar initialized to initial (see
// ParseLevel), for use as slog.HandlerOptions.Level. A *slog.LevelVar is
// mutable after the handler is built — LevelHandler (debug_handler.go) is
// how the gateway changes it at runtime (F-23 AC: "UDAL_LOG_LEVEL=debug
// enables debug logs without restart").
func NewLevelVar(initial string) (*slog.LevelVar, error) {
	lvl, err := ParseLevel(initial)
	if err != nil {
		return nil, err
	}
	v := &slog.LevelVar{}
	v.Set(lvl)
	return v, nil
}
