package capability_test

import (
	"errors"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/capability"
)

func mustParse(t *testing.T, raw []byte) capability.Schema {
	t.Helper()
	s, err := capability.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

const validationSchema = `{
	"udal": "1.0",
	"kind": "DeviceCapability",
	"metadata": {"name": "widget", "version": "1.0.0"},
	"definitions": {
		"mode": {"type": "object", "properties": {"enabled": {"type": "bool"}}}
	},
	"properties": {
		"level":       {"type": "int", "min": 0, "max": 100},
		"temperature": {"type": "float", "min": -40.0, "max": 125.0},
		"label":       {"type": "string"},
		"payload":     {"type": "bytes"},
		"unit":        {"type": "enum", "values": ["celsius", "fahrenheit"]},
		"mode_ref":    {"$ref": "#/definitions/mode"}
	}
}`

func TestValidateProperty_TypeMatches(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	cases := []struct {
		path string
		v    api.PropertyValue
	}{
		{"level", api.IntValue(50)},
		{"temperature", api.FloatValue(20.5)},
		{"label", api.StringValue("hello")},
		{"payload", api.PropertyValue{BytesVal: []byte{1, 2, 3}}},
		{"unit", api.StringValue("celsius")},
	}
	for _, c := range cases {
		if err := s.ValidateProperty(c.path, c.v); err != nil {
			t.Errorf("ValidateProperty(%s, %+v) = %v, want nil", c.path, c.v, err)
		}
	}
}

func TestValidateProperty_TypeMismatch(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	cases := []struct {
		name string
		path string
		v    api.PropertyValue
	}{
		{"string for float", "temperature", api.StringValue("hot")},
		{"float for int", "level", api.FloatValue(1.5)},
		{"bool for string", "label", api.BoolValue(true)},
		{"int for enum", "unit", api.IntValue(1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.ValidateProperty(c.path, c.v)
			if !errors.Is(err, capability.ErrInvalidPropertyValue) {
				t.Errorf("ValidateProperty(%s, %+v) = %v, want ErrInvalidPropertyValue", c.path, c.v, err)
			}
		})
	}
}

func TestValidateProperty_RangeViolation(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	cases := []struct {
		name string
		path string
		v    api.PropertyValue
	}{
		{"int below min", "level", api.IntValue(-1)},
		{"int above max", "level", api.IntValue(101)},
		{"float below min", "temperature", api.FloatValue(-41)},
		{"float above max", "temperature", api.FloatValue(126)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.ValidateProperty(c.path, c.v)
			if !errors.Is(err, capability.ErrInvalidPropertyValue) {
				t.Errorf("ValidateProperty(%s, %+v) = %v, want ErrInvalidPropertyValue", c.path, c.v, err)
			}
		})
	}
}

func TestValidateProperty_RangeBoundaryValuesAccepted(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	if err := s.ValidateProperty("level", api.IntValue(0)); err != nil {
		t.Errorf("min boundary rejected: %v", err)
	}
	if err := s.ValidateProperty("level", api.IntValue(100)); err != nil {
		t.Errorf("max boundary rejected: %v", err)
	}
}

func TestValidateProperty_EnumOutsideDeclaredValues(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	err := s.ValidateProperty("unit", api.StringValue("kelvin"))
	if !errors.Is(err, capability.ErrInvalidPropertyValue) {
		t.Errorf("ValidateProperty(unit, kelvin) = %v, want ErrInvalidPropertyValue", err)
	}
}

func TestValidateProperty_UnknownPropertyPath(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	err := s.ValidateProperty("nonexistent", api.IntValue(1))
	if !errors.Is(err, capability.ErrPropertyNotDeclared) {
		t.Errorf("ValidateProperty(nonexistent) = %v, want ErrPropertyNotDeclared", err)
	}
}

func TestValidateProperty_ResolvesRefBeforeValidating(t *testing.T) {
	s := mustParse(t, []byte(validationSchema))
	// mode_ref resolves to an object type -- currently unvalidated
	// (structured values aren't representable in api.PropertyValue yet),
	// so any value passes; this just confirms $ref resolution doesn't
	// error out during validation.
	if err := s.ValidateProperty("mode_ref", api.BoolValue(true)); err != nil {
		t.Errorf("ValidateProperty(mode_ref): %v", err)
	}
}
