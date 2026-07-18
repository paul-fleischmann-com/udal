package capability

import (
	"errors"
	"fmt"
	"log/slog"
)

// ErrNotFound is returned by Get when no schema exists for the given
// name+version.
var ErrNotFound = errors.New("capability: schema not found")

// ErrAlreadyExists is returned by Publish when the exact name+version is
// already published — schemas are immutable once published (F-13).
var ErrAlreadyExists = errors.New("capability: schema already exists")

// ErrInvalidSchema is returned by Publish when raw does not conform to the
// UDAL meta-schema (F-13's "invalid schema rejected with INVALID_ARGUMENT").
// Wraps the underlying validation error from ValidateAgainstMetaSchema.
var ErrInvalidSchema = errors.New("capability: schema is invalid")

// Registry stores and retrieves published capability Schemas by
// name+version. Implementations must be safe for concurrent use.
type Registry interface {
	// Publish validates raw against the UDAL meta-schema, parses it, and
	// stores it under its declared name@version (ErrInvalidSchema,
	// ErrAlreadyExists). If a schema with the same name and major version
	// already exists and appears less than the new one, and the new
	// version looks like it removed/retyped something the old one
	// declared, a warning is logged (not an error — F-14's "compatibility
	// warning logged if device declares minor-version mismatch" /
	// #22's "warn on breaking changes between minor versions").
	Publish(raw []byte) (Schema, error)
	// Get returns the schema for exactly name@version, or ErrNotFound.
	Get(name, version string) (Schema, error)
	// List returns every published schema, optionally filtered by name
	// (empty name: no filter).
	List(name string) ([]Schema, error)
}

func publishKey(name, version string) string { return name + "@" + version }

// validateAndParse is the publish-time logic shared by every Registry
// implementation: meta-schema validation, then parsing.
func validateAndParse(raw []byte) (Schema, error) {
	if err := ValidateAgainstMetaSchema(raw); err != nil {
		return Schema{}, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	s, err := Parse(raw)
	if err != nil {
		return Schema{}, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}
	if s.Name == "" || s.Version == "" {
		return Schema{}, fmt.Errorf("%w: metadata.name and metadata.version are required", ErrInvalidSchema)
	}
	return s, nil
}

// Option configures a Registry constructor.
type Option func(*options)

type options struct {
	log *slog.Logger
}

func newOptions(opts []Option) options {
	o := options{log: slog.Default()}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithLogger overrides the registry's logger (default: slog.Default()),
// used for the breaking-change warning.
func WithLogger(log *slog.Logger) Option {
	return func(o *options) { o.log = log }
}
