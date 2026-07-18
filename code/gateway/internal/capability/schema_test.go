package capability_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/capability"
)

func repoRootExamples(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	matches, err := filepath.Glob(filepath.Join(repoRoot, "schema", "examples", "*.json"))
	if err != nil {
		t.Fatalf("glob schema/examples: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no example schemas found — is schema/examples/*.json missing?")
	}
	return matches
}

// TestValidateAgainstMetaSchema_AllExamplesValid shares the same example
// documents the Python-based CI job (check-jsonschema) validates, as a
// regression check that the Go-side validator agrees with it.
func TestValidateAgainstMetaSchema_AllExamplesValid(t *testing.T) {
	for _, path := range repoRootExamples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if err := capability.ValidateAgainstMetaSchema(raw); err != nil {
				t.Errorf("ValidateAgainstMetaSchema(%s): %v", path, err)
			}
		})
	}
}

func TestValidateAgainstMetaSchema_RejectsInvalidJSON(t *testing.T) {
	if err := capability.ValidateAgainstMetaSchema([]byte("not json")); err == nil {
		t.Error("expected an error for malformed JSON, got nil")
	}
}

func TestValidateAgainstMetaSchema_RejectsMissingRequiredFields(t *testing.T) {
	if err := capability.ValidateAgainstMetaSchema([]byte(`{"udal": "1.0", "kind": "DeviceCapability"}`)); err == nil {
		t.Error("expected an error for a schema missing required 'metadata', got nil")
	}
}

func TestValidateAgainstMetaSchema_RejectsWrongUDALVersion(t *testing.T) {
	doc := `{"udal": "2.0", "kind": "DeviceCapability", "metadata": {"name": "x", "version": "1.0.0"}}`
	if err := capability.ValidateAgainstMetaSchema([]byte(doc)); err == nil {
		t.Error("expected an error for udal != \"1.0\" (const), got nil")
	}
}

func TestParse_TemperatureSensorExample(t *testing.T) {
	var examplePath string
	for _, p := range repoRootExamples(t) {
		if filepath.Base(p) == "temperature-sensor.json" {
			examplePath = p
		}
	}
	if examplePath == "" {
		t.Fatal("temperature-sensor.json example not found")
	}
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	s, err := capability.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Name != "temperature-sensor" || s.Version != "1.0.0" {
		t.Errorf("Name/Version = %q/%q, want temperature-sensor/1.0.0", s.Name, s.Version)
	}

	temp, ok := s.Properties["temperature"]
	if !ok {
		t.Fatal("expected a 'temperature' property")
	}
	if temp.Type != "float" || temp.Min == nil || *temp.Min != -40.0 || temp.Max == nil || *temp.Max != 125.0 || !temp.ReadOnly {
		t.Errorf("temperature property = %+v, want type=float min=-40 max=125 readOnly=true", temp)
	}

	displayUnit, ok := s.Properties["display_unit"]
	if !ok {
		t.Fatal("expected a 'display_unit' property")
	}
	if displayUnit.Type != "enum" || len(displayUnit.Values) != 2 {
		t.Errorf("display_unit = %+v, want an enum with 2 values", displayUnit)
	}

	thresholdRef, ok := s.Properties["temp_threshold"]
	if !ok {
		t.Fatal("expected a 'temp_threshold' property")
	}
	resolved, err := s.ResolveType(thresholdRef.TypeDef)
	if err != nil {
		t.Fatalf("ResolveType(temp_threshold): %v", err)
	}
	if resolved.Type != "object" {
		t.Errorf("resolved temp_threshold.Type = %q, want object (from #/definitions/threshold)", resolved.Type)
	}
	if _, ok := resolved.Properties["direction"]; !ok {
		t.Errorf("resolved temp_threshold missing 'direction' field from the referenced definition")
	}

	calibrate, ok := s.Commands["calibrate"]
	if !ok {
		t.Fatal("expected a 'calibrate' command")
	}
	offset, ok := calibrate.Params["offset"]
	if !ok || !offset.Required || offset.Type != "float" {
		t.Errorf("calibrate.params.offset = %+v, want required float", offset)
	}
}

func TestResolveType_UndefinedReference(t *testing.T) {
	s := capability.Schema{Definitions: map[string]capability.TypeDef{}}
	_, err := s.ResolveType(capability.TypeDef{Ref: "#/definitions/missing"})
	if err == nil {
		t.Error("expected an error for an undefined $ref, got nil")
	}
}

func TestResolveType_CircularReference(t *testing.T) {
	s := capability.Schema{Definitions: map[string]capability.TypeDef{
		"a": {Ref: "#/definitions/b"},
		"b": {Ref: "#/definitions/a"},
	}}
	_, err := s.ResolveType(capability.TypeDef{Ref: "#/definitions/a"})
	if err == nil {
		t.Error("expected an error for a circular $ref chain, got nil")
	}
}

func TestResolveType_UnsupportedRefTarget(t *testing.T) {
	s := capability.Schema{}
	_, err := s.ResolveType(capability.TypeDef{Ref: "#/components/schemas/Foo"})
	if err == nil {
		t.Error("expected an error for a $ref outside #/definitions/, got nil")
	}
}

func TestParse_MalformedJSON(t *testing.T) {
	if _, err := capability.Parse([]byte("{not json")); err == nil {
		t.Error("expected an error, got nil")
	}
}
