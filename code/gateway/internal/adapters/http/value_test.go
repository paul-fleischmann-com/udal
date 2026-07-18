package httpadapter

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
		got := decodeValue(encodeValue(v))
		if !propertyValueEqual(v, got) {
			t.Errorf("round trip: got %+v, want %+v", got, v)
		}
	}
}

func TestSnapshotKey_SameValueSameKey(t *testing.T) {
	a := snapshotKey(encodeValue(api.FloatValue(21.5)))
	b := snapshotKey(encodeValue(api.FloatValue(21.5)))
	if a != b || a == "" {
		t.Errorf("snapshotKey not stable for equal values: %q vs %q", a, b)
	}
}

func TestSnapshotKey_DifferentValueDifferentKey(t *testing.T) {
	a := snapshotKey(encodeValue(api.FloatValue(21.5)))
	b := snapshotKey(encodeValue(api.FloatValue(21.6)))
	if a == b {
		t.Errorf("snapshotKey collided for different values: %q", a)
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
