package capability

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// metaSchemaJSON is a copy of schema/udal-capability.schema.json (repo
// root). go:embed can't reach outside this package's own directory tree,
// let alone across module boundaries into the sibling schema/ directory
// (and the final distroless Docker image doesn't contain schema/ at all —
// only the compiled binary — so reading it from a runtime file path
// wouldn't work in production either). metaschema_sync_test.go asserts
// this copy is byte-identical to the canonical file, so the two can't
// silently drift apart.
//
//go:embed metaschema/udal-capability.schema.json
var metaSchemaJSON []byte

// metaSchemaID matches the canonical file's own "$id" — the URL the
// compiler is asked to Compile is otherwise arbitrary, but reusing "$id"
// avoids any confusion between the resource's registered name and its
// self-declared identity.
const metaSchemaID = "https://github.com/paulefl/udal/schema/udal-capability.schema.json"

var compiledMetaSchema = sync.OnceValue(func() *jsonschema.Schema {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(metaSchemaID, bytes.NewReader(metaSchemaJSON)); err != nil {
		panic(fmt.Sprintf("capability: embedded meta-schema failed to load: %v", err))
	}
	return compiler.MustCompile(metaSchemaID)
})

// ValidateAgainstMetaSchema validates raw (a capability schema document)
// against the UDAL meta-schema (F-13: "invalid schema rejected with
// INVALID_ARGUMENT").
func ValidateAgainstMetaSchema(raw []byte) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("capability: invalid JSON: %w", err)
	}
	if err := compiledMetaSchema().Validate(doc); err != nil {
		return fmt.Errorf("capability: schema does not conform to the UDAL meta-schema: %w", err)
	}
	return nil
}
