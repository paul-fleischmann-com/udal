package logging

import (
	"context"
	"testing"
)

func TestGenerateTraceID_Shape(t *testing.T) {
	id := GenerateTraceID()
	if len(id) != 32 {
		t.Fatalf("len(GenerateTraceID()) = %d, want 32 (16 bytes hex-encoded, W3C/OTEL TraceID shape)", len(id))
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("GenerateTraceID() = %q, contains non-lowercase-hex character %q", id, r)
		}
	}
}

func TestGenerateTraceID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateTraceID()
		if seen[id] {
			t.Fatalf("GenerateTraceID() produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestWithTraceID_Roundtrip(t *testing.T) {
	ctx := WithTraceID(context.Background(), "abc123")
	got, ok := TraceIDFromContext(ctx)
	if !ok || got != "abc123" {
		t.Errorf("TraceIDFromContext = (%q, %v), want (\"abc123\", true)", got, ok)
	}
}

func TestTraceIDFromContext_Absent(t *testing.T) {
	_, ok := TraceIDFromContext(context.Background())
	if ok {
		t.Error("TraceIDFromContext on a context with no trace ID: want ok=false")
	}
}
