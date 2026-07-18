package logging

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"  debug  ", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseLevel(tt.in)
			if err != nil {
				t.Fatalf("ParseLevel(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseLevel_Invalid(t *testing.T) {
	if _, err := ParseLevel("verbose"); err == nil {
		t.Fatal("ParseLevel(\"verbose\"): want error, got nil")
	}
}

func TestNewLevelVar(t *testing.T) {
	v, err := NewLevelVar("debug")
	if err != nil {
		t.Fatalf("NewLevelVar: %v", err)
	}
	if v.Level() != slog.LevelDebug {
		t.Errorf("Level() = %v, want debug", v.Level())
	}
	// The whole point of a *slog.LevelVar is that it's mutable after
	// construction (see LevelHandler) — verify Set actually takes effect.
	v.Set(slog.LevelError)
	if v.Level() != slog.LevelError {
		t.Errorf("Level() after Set = %v, want error", v.Level())
	}
}

func TestNewLevelVar_Invalid(t *testing.T) {
	if _, err := NewLevelVar("nonsense"); err == nil {
		t.Fatal("NewLevelVar(\"nonsense\"): want error, got nil")
	}
}
