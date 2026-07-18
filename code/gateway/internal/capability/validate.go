package capability

import (
	"errors"
	"fmt"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// ErrPropertyNotDeclared is returned by Schema.ValidateProperty when path
// isn't declared in the schema at all.
var ErrPropertyNotDeclared = errors.New("capability: property not declared in schema")

// ErrInvalidPropertyValue is returned by Schema.ValidateProperty when the
// value doesn't conform to the property's declared type, range, or (for
// enums) allowed values (F-15).
var ErrInvalidPropertyValue = errors.New("capability: property value does not conform to its declared type")

// ValidateProperty checks v against path's declared PropertyDef in s
// (resolving a $ref first, if the property is defined that way).
func (s Schema) ValidateProperty(path string, v api.PropertyValue) error {
	def, ok := s.Properties[path]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPropertyNotDeclared, path)
	}
	resolved, err := s.ResolveType(def.TypeDef)
	if err != nil {
		return err
	}
	return validateValue(path, resolved, v)
}

func validateValue(path string, t TypeDef, v api.PropertyValue) error {
	switch t.Type {
	case "bool":
		if v.BoolVal == nil {
			return fmt.Errorf("%w: property %q expects bool", ErrInvalidPropertyValue, path)
		}
	case "int":
		if v.IntVal == nil {
			return fmt.Errorf("%w: property %q expects int", ErrInvalidPropertyValue, path)
		}
		return checkRange(path, t, float64(*v.IntVal))
	case "float":
		if v.FloatVal == nil {
			return fmt.Errorf("%w: property %q expects float", ErrInvalidPropertyValue, path)
		}
		return checkRange(path, t, *v.FloatVal)
	case "string":
		if v.StringVal == nil {
			return fmt.Errorf("%w: property %q expects string", ErrInvalidPropertyValue, path)
		}
	case "bytes":
		if v.BytesVal == nil {
			return fmt.Errorf("%w: property %q expects bytes", ErrInvalidPropertyValue, path)
		}
	case "enum":
		if v.StringVal == nil {
			return fmt.Errorf("%w: property %q expects a string enum value", ErrInvalidPropertyValue, path)
		}
		if !containsString(t.Values, *v.StringVal) {
			return fmt.Errorf("%w: property %q value %q is not one of %v", ErrInvalidPropertyValue, path, *v.StringVal, t.Values)
		}
	case "array", "object":
		// api.PropertyValue has no structured array/object representation
		// yet (device_service.go's toProtoValue: "JSON property values not
		// yet supported in proto mapping") -- nothing to validate against
		// until that exists; accept anything rather than reject a type the
		// wire format can't even carry yet.
	default:
		return fmt.Errorf("capability: property %q has unsupported type %q", path, t.Type)
	}
	return nil
}

func checkRange(path string, t TypeDef, val float64) error {
	if t.Min != nil && val < *t.Min {
		return fmt.Errorf("%w: property %q value %v is below minimum %v", ErrInvalidPropertyValue, path, val, *t.Min)
	}
	if t.Max != nil && val > *t.Max {
		return fmt.Errorf("%w: property %q value %v is above maximum %v", ErrInvalidPropertyValue, path, val, *t.Max)
	}
	return nil
}

func containsString(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
