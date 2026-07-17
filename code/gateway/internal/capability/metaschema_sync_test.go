package capability_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEmbeddedMetaSchemaMatchesCanonicalFile guards against the embedded
// copy (metaschema/udal-capability.schema.json, needed because go:embed
// can't reach the canonical file at the repo root — see metaschema.go)
// silently drifting from schema/udal-capability.schema.json, the source of
// truth referenced by docs and the Python-based CI validation of
// schema/examples/*.json.
func TestEmbeddedMetaSchemaMatchesCanonicalFile(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file: code/gateway/internal/capability/metaschema_sync_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	canonicalPath := filepath.Join(repoRoot, "schema", "udal-capability.schema.json")
	embeddedPath := filepath.Join(filepath.Dir(thisFile), "metaschema", "udal-capability.schema.json")

	canonical, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read canonical schema %s: %v", canonicalPath, err)
	}
	embedded, err := os.ReadFile(embeddedPath)
	if err != nil {
		t.Fatalf("read embedded schema %s: %v", embeddedPath, err)
	}
	if string(canonical) != string(embedded) {
		t.Errorf("embedded meta-schema copy (%s) has drifted from the canonical file (%s) -- "+
			"re-copy the canonical file over the embedded one", embeddedPath, canonicalPath)
	}
}
