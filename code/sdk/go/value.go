package udal

import (
	"fmt"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
)

// valueFromProto converts a PropertyValue to a Go native value: bool, int64,
// float64, string, or []byte.
func valueFromProto(pv *udalv1.PropertyValue) any {
	switch v := pv.GetValue().(type) {
	case *udalv1.PropertyValue_BoolVal:
		return v.BoolVal
	case *udalv1.PropertyValue_IntVal:
		return v.IntVal
	case *udalv1.PropertyValue_FloatVal:
		return v.FloatVal
	case *udalv1.PropertyValue_StringVal:
		return v.StringVal
	case *udalv1.PropertyValue_BytesVal:
		return v.BytesVal
	default:
		return nil
	}
}

// valueToProto converts a Go native value (bool, int, int64, float32,
// float64, string, or []byte) into a PropertyValue. Structured (JSON)
// values aren't supported here — the gateway's own property storage
// doesn't round-trip them correctly yet (see device_service.go's
// toProtoValue/fromProtoValue), so accepting them here would silently
// produce broken behavior rather than a clear error.
func valueToProto(value any) (*udalv1.PropertyValue, error) {
	switch v := value.(type) {
	case bool:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_BoolVal{BoolVal: v}}, nil
	case int:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_IntVal{IntVal: int64(v)}}, nil
	case int64:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_IntVal{IntVal: v}}, nil
	case float32:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: float64(v)}}, nil
	case float64:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: v}}, nil
	case string:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_StringVal{StringVal: v}}, nil
	case []byte:
		return &udalv1.PropertyValue{Value: &udalv1.PropertyValue_BytesVal{BytesVal: v}}, nil
	default:
		return nil, fmt.Errorf("unsupported property value type %T (supported: bool, int, int64, float32, float64, string, []byte)", value)
	}
}
