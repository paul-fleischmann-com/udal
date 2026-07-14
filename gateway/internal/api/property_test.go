package api_test

import (
	"errors"
	"testing"

	"github.com/paulefl/udal/gateway/internal/api"
)

func TestMemoryPropertyStore_SetGet(t *testing.T) {
	s := api.NewMemoryPropertyStore()

	if err := s.Set("dev-1", "temperature", api.FloatValue(22.5)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get("dev-1", "temperature")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FloatVal == nil || *got.FloatVal != 22.5 {
		t.Errorf("FloatVal = %v, want 22.5", got.FloatVal)
	}
}

func TestMemoryPropertyStore_GetMissing(t *testing.T) {
	s := api.NewMemoryPropertyStore()
	_, err := s.Get("dev-1", "humidity")
	if err == nil {
		t.Fatal("expected error for missing property, got nil")
	}
}

func TestMemoryPropertyStore_Overwrite(t *testing.T) {
	s := api.NewMemoryPropertyStore()
	if err := s.Set("dev-1", "temperature", api.FloatValue(20.0)); err != nil {
		t.Fatalf("Set 20.0: %v", err)
	}
	if err := s.Set("dev-1", "temperature", api.FloatValue(25.0)); err != nil {
		t.Fatalf("Set 25.0: %v", err)
	}

	got, _ := s.Get("dev-1", "temperature")
	if got.FloatVal == nil || *got.FloatVal != 25.0 {
		t.Errorf("overwrite: FloatVal = %v, want 25.0", got.FloatVal)
	}
}

func TestConvenienceConstructors(t *testing.T) {
	b := true
	i := int64(42)
	f := 3.14
	str := "hello"

	tests := []struct {
		name  string
		value api.PropertyValue
		check func(api.PropertyValue) bool
	}{
		{"bool", api.BoolValue(b), func(v api.PropertyValue) bool { return v.BoolVal != nil && *v.BoolVal == b }},
		{"int", api.IntValue(i), func(v api.PropertyValue) bool { return v.IntVal != nil && *v.IntVal == i }},
		{"float", api.FloatValue(f), func(v api.PropertyValue) bool { return v.FloatVal != nil && *v.FloatVal == f }},
		{"string", api.StringValue(str), func(v api.PropertyValue) bool { return v.StringVal != nil && *v.StringVal == str }},
	}

	for _, tt := range tests {
		if !tt.check(tt.value) {
			t.Errorf("%s: value check failed", tt.name)
		}
	}
}

func TestJSONValue(t *testing.T) {
	v, err := api.JSONValue(map[string]any{"x": 1, "y": 2})
	if err != nil {
		t.Fatalf("JSONValue: %v", err)
	}
	if len(v.JSONVal) == 0 {
		t.Error("JSONVal is empty")
	}

	_, err = api.JSONValue(make(chan int))
	if err == nil {
		t.Error("expected error for non-serialisable value")
	}
	_ = errors.New("ok") // suppress unused import
}
