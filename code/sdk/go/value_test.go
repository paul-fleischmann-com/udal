package udal

import (
	"reflect"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
)

func TestValueRoundTrip(t *testing.T) {
	tests := []any{true, 42, int64(42), float32(1.5), 21.5, "hello", []byte("bytes")}
	for _, in := range tests {
		pv, err := valueToProto(in)
		if err != nil {
			t.Fatalf("valueToProto(%v): %v", in, err)
		}
		out := valueFromProto(pv)

		want := in
		switch v := in.(type) {
		case int:
			want = int64(v)
		case float32:
			want = float64(v)
		}
		if !reflect.DeepEqual(out, want) {
			t.Errorf("round trip of %v (%T): got %v (%T), want %v", in, in, out, out, want)
		}
	}
}

func TestValueToProto_Unsupported(t *testing.T) {
	_, err := valueToProto(struct{ X int }{X: 1})
	if err == nil {
		t.Fatal("expected an error for an unsupported type")
	}
}

func TestValueFromProto_Empty(t *testing.T) {
	if got := valueFromProto(&udalv1.PropertyValue{}); got != nil {
		t.Errorf("expected nil for an empty PropertyValue, got %v", got)
	}
}
