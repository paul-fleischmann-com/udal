package capability_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/capability"
	"go.etcd.io/bbolt"
)

func minimalSchema(name, version, properties string) []byte {
	return []byte(fmt.Sprintf(`{
		"udal": "1.0",
		"kind": "DeviceCapability",
		"metadata": {"name": %q, "version": %q},
		"properties": {%s}
	}`, name, version, properties))
}

func newBboltRegistryForTest(t *testing.T, opts ...capability.Option) *capability.BboltRegistry {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "test.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	reg, err := capability.NewBboltRegistry(db, opts...)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	return reg
}

func mustPublish(t *testing.T, reg capability.Registry, raw []byte) capability.Schema {
	t.Helper()
	s, err := reg.Publish(raw)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return s
}

// registries returns both backends under test, so shared correctness tests
// (everything except the bbolt-specific persistence test) run against each.
func registries(t *testing.T) map[string]capability.Registry {
	t.Helper()
	return map[string]capability.Registry{
		"memory": capability.NewMemoryRegistry(),
		"bbolt":  newBboltRegistryForTest(t),
	}
}

func TestRegistry_PublishAndGet(t *testing.T) {
	for name, reg := range registries(t) {
		t.Run(name, func(t *testing.T) {
			raw := minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`)
			published := mustPublish(t, reg, raw)
			if published.Name != "widget" || published.Version != "1.0.0" {
				t.Errorf("published = %+v", published)
			}

			got, err := reg.Get("widget", "1.0.0")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Name != "widget" || got.Properties["level"].Type != "int" {
				t.Errorf("Get = %+v", got)
			}
		})
	}
}

func TestRegistry_PublishDuplicateRejected(t *testing.T) {
	for name, reg := range registries(t) {
		t.Run(name, func(t *testing.T) {
			raw := minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`)
			mustPublish(t, reg, raw)
			_, err := reg.Publish(raw)
			if !errors.Is(err, capability.ErrAlreadyExists) {
				t.Errorf("second Publish error = %v, want ErrAlreadyExists", err)
			}
		})
	}
}

func TestRegistry_PublishInvalidSchemaRejected(t *testing.T) {
	for name, reg := range registries(t) {
		t.Run(name, func(t *testing.T) {
			_, err := reg.Publish([]byte(`{"udal": "1.0", "kind": "DeviceCapability"}`)) // missing required metadata
			if !errors.Is(err, capability.ErrInvalidSchema) {
				t.Errorf("Publish error = %v, want ErrInvalidSchema", err)
			}
		})
	}
}

func TestRegistry_GetUnknownReturnsNotFound(t *testing.T) {
	for name, reg := range registries(t) {
		t.Run(name, func(t *testing.T) {
			_, err := reg.Get("missing", "1.0.0")
			if !errors.Is(err, capability.ErrNotFound) {
				t.Errorf("Get error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestRegistry_ListFiltersByName(t *testing.T) {
	for name, reg := range registries(t) {
		t.Run(name, func(t *testing.T) {
			mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))
			mustPublish(t, reg, minimalSchema("gadget", "1.0.0", `"level": {"type": "int"}`))

			all, err := reg.List("")
			if err != nil || len(all) != 2 {
				t.Fatalf("List(\"\") = %v, %v, want 2 schemas", all, err)
			}
			widgets, err := reg.List("widget")
			if err != nil || len(widgets) != 1 || widgets[0].Name != "widget" {
				t.Fatalf("List(\"widget\") = %v, %v, want [widget]", widgets, err)
			}
		})
	}
}

func TestRegistry_BboltPersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	reg, err := capability.NewBboltRegistry(db)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	db2, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen bbolt: %v", err)
	}
	defer func() { _ = db2.Close() }()
	reg2, err := capability.NewBboltRegistry(db2)
	if err != nil {
		t.Fatalf("NewBboltRegistry (reopen): %v", err)
	}
	got, err := reg2.Get("widget", "1.0.0")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Name != "widget" {
		t.Errorf("Get after reopen = %+v", got)
	}
}

func TestMemoryRegistry_WarnsOnBreakingChangeWithinSameMajor(t *testing.T) {
	var buf bytes.Buffer
	reg := capability.NewMemoryRegistry(capability.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))
	mustPublish(t, reg, minimalSchema("widget", "1.1.0", ``)) // removed "level" -- breaking, but only a minor bump

	if !bytes.Contains(buf.Bytes(), []byte("potentially breaking change")) {
		t.Errorf("expected a breaking-change warning to be logged, got: %s", buf.String())
	}
}

func TestBboltRegistry_WarnsOnBreakingChangeWithinSameMajor(t *testing.T) {
	var buf bytes.Buffer
	reg := newBboltRegistryForTest(t, capability.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))
	mustPublish(t, reg, minimalSchema("widget", "1.1.0", ``)) // removed "level" -- breaking, but only a minor bump

	if !bytes.Contains(buf.Bytes(), []byte("potentially breaking change")) {
		t.Errorf("expected a breaking-change warning to be logged, got: %s", buf.String())
	}
}

func TestRegistry_NoWarningForNonBreakingChange(t *testing.T) {
	var buf bytes.Buffer
	reg := capability.NewMemoryRegistry(capability.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))
	mustPublish(t, reg, minimalSchema("widget", "1.1.0", `"level": {"type": "int"}, "extra": {"type": "bool"}`)) // added only

	if bytes.Contains(buf.Bytes(), []byte("potentially breaking change")) {
		t.Errorf("unexpected breaking-change warning for an additive change: %s", buf.String())
	}
}

func TestRegistry_NoWarningAcrossMajorVersionBump(t *testing.T) {
	var buf bytes.Buffer
	reg := capability.NewMemoryRegistry(capability.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))
	mustPublish(t, reg, minimalSchema("widget", "2.0.0", ``)) // removed "level", but a MAJOR bump is allowed to break

	if bytes.Contains(buf.Bytes(), []byte("potentially breaking change")) {
		t.Errorf("unexpected breaking-change warning across a major version bump: %s", buf.String())
	}
}

func TestRegistry_NoWarningWhenNoPriorVersionExists(t *testing.T) {
	var buf bytes.Buffer
	reg := capability.NewMemoryRegistry(capability.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	mustPublish(t, reg, minimalSchema("widget", "1.0.0", `"level": {"type": "int"}`))

	if bytes.Contains(buf.Bytes(), []byte("potentially breaking change")) {
		t.Errorf("unexpected breaking-change warning for the first published version: %s", buf.String())
	}
}
