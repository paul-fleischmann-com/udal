package httpadapter

import (
	"encoding/json"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// wireValue is the JSON envelope used for api.PropertyValue on the wire —
// independent of mqtt's own wireValue (adapters/mqtt/value.go): the two
// transports' device-side wire formats aren't required to match, and
// coupling them through a shared type would only make one adapter's future
// changes a breaking change for the other.
type wireValue struct {
	Bool   *bool    `json:"bool,omitempty"`
	Int    *int64   `json:"int,omitempty"`
	Float  *float64 `json:"float,omitempty"`
	String *string  `json:"string,omitempty"`
	Bytes  []byte   `json:"bytes,omitempty"`
}

func encodeValue(v api.PropertyValue) wireValue {
	return wireValue{Bool: v.BoolVal, Int: v.IntVal, Float: v.FloatVal, String: v.StringVal, Bytes: v.BytesVal}
}

func decodeValue(w wireValue) api.PropertyValue {
	return api.PropertyValue{BoolVal: w.Bool, IntVal: w.Int, FloatVal: w.Float, StringVal: w.String, BytesVal: w.Bytes}
}

// snapshotKey returns a value equal for two wireValues with equal content —
// used by the poller (poller.go) to detect whether a property actually
// changed since the last tick, since wireValue's pointer fields make it
// non-comparable with ==.
func snapshotKey(w wireValue) string {
	b, err := json.Marshal(w)
	if err != nil {
		return ""
	}
	return string(b)
}
