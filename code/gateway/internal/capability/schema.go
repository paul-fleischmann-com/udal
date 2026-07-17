// Package capability implements the Capability Registry service (F-13/
// F-14/F-15, GitHub issue #22): stores, versions, and serves capability
// schemas, and validates devices/property writes against them.
package capability

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Schema is a parsed capability schema document (schema/udal-capability.schema.json).
type Schema struct {
	Name        string
	Version     string
	Description string
	// Raw is the exact document as published/validated — the source of
	// truth returned to callers, never re-derived from the parsed fields
	// below.
	Raw []byte

	Definitions map[string]TypeDef
	Properties  map[string]PropertyDef
	Commands    map[string]CommandDef
	Events      map[string]EventDef
}

// TypeDef is a property/parameter/field type definition — the meta-schema's
// polymorphic "oneOf" TypeDef, flattened into one struct with UnmarshalJSON
// picking out whichever fields apply (Type == "" means this is a $ref,
// resolved via Schema.ResolveType).
type TypeDef struct {
	Type        string // "bool"|"int"|"float"|"string"|"bytes"|"enum"|"array"|"object"; "" if Ref is set
	Ref         string // "#/definitions/{name}", only for a $ref TypeDef
	Description string
	Unit        string
	Min         *float64
	Max         *float64
	Default     json.RawMessage
	Values      []string           // enum
	Items       *TypeDef           // array
	Properties  map[string]TypeDef // object
	Required    []string           // object
}

func (t *TypeDef) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type        string                     `json:"type"`
		Ref         string                     `json:"$ref"`
		Description string                     `json:"description"`
		Unit        string                     `json:"unit"`
		Min         *float64                   `json:"min"`
		Max         *float64                   `json:"max"`
		Default     json.RawMessage            `json:"default"`
		Values      []string                   `json:"values"`
		Items       json.RawMessage            `json:"items"`
		Properties  map[string]json.RawMessage `json:"properties"`
		// Required is only meaningful for an ObjectTypeDef ("required
		// field names"); PropertyDef/ParamDef also use the JSON key
		// "required" for an unrelated purpose (a bool: "is this
		// property/param mandatory"), which they unmarshal separately —
		// captured as raw here so a non-object TypeDef with a boolean
		// "required" (e.g. a command parameter) doesn't fail to unmarshal.
		Required json.RawMessage `json:"required"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.Type, t.Ref, t.Description, t.Unit = raw.Type, raw.Ref, raw.Description, raw.Unit
	t.Min, t.Max, t.Default, t.Values = raw.Min, raw.Max, raw.Default, raw.Values
	if t.Type == "object" && len(raw.Required) > 0 {
		if err := json.Unmarshal(raw.Required, &t.Required); err != nil {
			return fmt.Errorf("required: %w", err)
		}
	}
	if len(raw.Items) > 0 {
		var items TypeDef
		if err := json.Unmarshal(raw.Items, &items); err != nil {
			return fmt.Errorf("items: %w", err)
		}
		t.Items = &items
	}
	if len(raw.Properties) > 0 {
		t.Properties = make(map[string]TypeDef, len(raw.Properties))
		for name, v := range raw.Properties {
			var pt TypeDef
			if err := json.Unmarshal(v, &pt); err != nil {
				return fmt.Errorf("properties[%s]: %w", name, err)
			}
			t.Properties[name] = pt
		}
	}
	return nil
}

// PropertyDef is a device property: a TypeDef plus readOnly/writeOnly.
type PropertyDef struct {
	TypeDef
	ReadOnly  bool
	WriteOnly bool
}

func (p *PropertyDef) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &p.TypeDef); err != nil {
		return err
	}
	var extra struct {
		ReadOnly  bool `json:"readOnly"`
		WriteOnly bool `json:"writeOnly"`
	}
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	p.ReadOnly, p.WriteOnly = extra.ReadOnly, extra.WriteOnly
	return nil
}

// ParamDef is a command parameter: a TypeDef plus required.
type ParamDef struct {
	TypeDef
	Required bool
}

func (p *ParamDef) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &p.TypeDef); err != nil {
		return err
	}
	var extra struct {
		Required bool `json:"required"`
	}
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	p.Required = extra.Required
	return nil
}

type CommandDef struct {
	Description string
	Params      map[string]ParamDef
	Returns     *TypeDef
}

type EventDef struct {
	Description string
	Payload     map[string]TypeDef
}

// Parse decodes a capability schema document. It does not validate against
// the meta-schema — call ValidateAgainstMetaSchema first (Publish always
// does both, in that order).
func Parse(raw []byte) (Schema, error) {
	var doc struct {
		Metadata struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"metadata"`
		Definitions map[string]TypeDef     `json:"definitions"`
		Properties  map[string]PropertyDef `json:"properties"`
		Commands    map[string]CommandDef  `json:"commands"`
		Events      map[string]EventDef    `json:"events"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Schema{}, fmt.Errorf("capability: parse schema: %w", err)
	}
	return Schema{
		Name:        doc.Metadata.Name,
		Version:     doc.Metadata.Version,
		Description: doc.Metadata.Description,
		Raw:         raw,
		Definitions: doc.Definitions,
		Properties:  doc.Properties,
		Commands:    doc.Commands,
		Events:      doc.Events,
	}, nil
}

// ResolveType follows t's $ref chain (against s.Definitions) until it
// reaches a concrete (non-$ref) TypeDef, detecting cycles.
func (s Schema) ResolveType(t TypeDef) (TypeDef, error) {
	seen := make(map[string]bool)
	for t.Ref != "" {
		if seen[t.Ref] {
			return TypeDef{}, fmt.Errorf("capability: circular $ref: %s", t.Ref)
		}
		seen[t.Ref] = true
		const prefix = "#/definitions/"
		if !strings.HasPrefix(t.Ref, prefix) {
			return TypeDef{}, fmt.Errorf("capability: unsupported $ref (only %s... is): %s", prefix, t.Ref)
		}
		name := strings.TrimPrefix(t.Ref, prefix)
		resolved, ok := s.Definitions[name]
		if !ok {
			return TypeDef{}, fmt.Errorf("capability: undefined reference: %s", t.Ref)
		}
		t = resolved
	}
	return t, nil
}
