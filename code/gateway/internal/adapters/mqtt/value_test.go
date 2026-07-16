package mqtt

import (
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

func TestEncodeDecodeValueRoundTrip(t *testing.T) {
	cases := []api.PropertyValue{
		api.BoolValue(true),
		api.BoolValue(false),
		api.IntValue(42),
		api.FloatValue(21.5),
		api.StringValue("hello"),
		{BytesVal: []byte{1, 2, 3}},
	}
	for _, v := range cases {
		payload, err := encodeValue(v)
		if err != nil {
			t.Fatalf("encodeValue(%+v): %v", v, err)
		}
		got, err := decodeValue(payload)
		if err != nil {
			t.Fatalf("decodeValue(%s): %v", payload, err)
		}
		if !propertyValueEqual(v, got) {
			t.Errorf("round trip: got %+v, want %+v (wire: %s)", got, v, payload)
		}
	}
}

func TestDecodeValueMalformedPayload(t *testing.T) {
	if _, err := decodeValue([]byte("not json")); err == nil {
		t.Error("expected error decoding malformed payload, got nil")
	}
}

func propertyValueEqual(a, b api.PropertyValue) bool {
	eq := func(x, y *bool) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqI := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqF := func(x, y *float64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqS := func(x, y *string) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	if !eq(a.BoolVal, b.BoolVal) || !eqI(a.IntVal, b.IntVal) || !eqF(a.FloatVal, b.FloatVal) || !eqS(a.StringVal, b.StringVal) {
		return false
	}
	if len(a.BytesVal) != len(b.BytesVal) {
		return false
	}
	for i := range a.BytesVal {
		if a.BytesVal[i] != b.BytesVal[i] {
			return false
		}
	}
	return true
}
