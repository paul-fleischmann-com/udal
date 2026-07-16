package mqtt

import (
	"encoding/json"
	"fmt"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// wireValue is the JSON envelope used for api.PropertyValue on the wire.
// Exactly one field is set, mirroring api.PropertyValue's discriminated
// union (JSONVal isn't included: the proto mapping doesn't support it yet
// either, see toProtoValue in internal/service).
type wireValue struct {
	Bool   *bool    `json:"bool,omitempty"`
	Int    *int64   `json:"int,omitempty"`
	Float  *float64 `json:"float,omitempty"`
	String *string  `json:"string,omitempty"`
	Bytes  []byte   `json:"bytes,omitempty"`
}

func encodeValue(v api.PropertyValue) ([]byte, error) {
	w := wireValue{Bool: v.BoolVal, Int: v.IntVal, Float: v.FloatVal, String: v.StringVal, Bytes: v.BytesVal}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("mqtt: encode property value: %w", err)
	}
	return b, nil
}

func decodeValue(payload []byte) (api.PropertyValue, error) {
	var w wireValue
	if err := json.Unmarshal(payload, &w); err != nil {
		return api.PropertyValue{}, fmt.Errorf("mqtt: decode property value: %w", err)
	}
	return api.PropertyValue{BoolVal: w.Bool, IntVal: w.Int, FloatVal: w.Float, StringVal: w.String, BytesVal: w.Bytes}, nil
}
