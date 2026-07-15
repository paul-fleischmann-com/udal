package api

import (
	"encoding/json"
	"fmt"
	"sync"
)

// PropertyStore stores current property values per device.
// In production this is backed by the transport adapter; the in-memory
// implementation is used by unit tests.
type PropertyStore interface {
	Get(deviceID, propertyPath string) (PropertyValue, error)
	Set(deviceID, propertyPath string, v PropertyValue) error
}

// MemoryPropertyStore is a thread-safe in-memory PropertyStore for tests.
type MemoryPropertyStore struct {
	mu   sync.RWMutex
	data map[string]PropertyValue // key: "deviceID/propertyPath"
}

// NewMemoryPropertyStore returns an empty, thread-safe in-memory property store.
func NewMemoryPropertyStore() *MemoryPropertyStore {
	return &MemoryPropertyStore{data: make(map[string]PropertyValue)}
}

func (s *MemoryPropertyStore) key(deviceID, propertyPath string) string {
	return deviceID + "/" + propertyPath
}

// Get returns the current value for the given device property.
// Returns an error if the property has not been set.
func (s *MemoryPropertyStore) Get(deviceID, propertyPath string) (PropertyValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[s.key(deviceID, propertyPath)]
	if !ok {
		return PropertyValue{}, fmt.Errorf("property %q not found on device %q", propertyPath, deviceID)
	}
	return v, nil
}

// Set stores a property value for the given device.
func (s *MemoryPropertyStore) Set(deviceID, propertyPath string, v PropertyValue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[s.key(deviceID, propertyPath)] = v
	return nil
}

// FloatValue constructs a PropertyValue holding a float64.
func FloatValue(f float64) PropertyValue { return PropertyValue{FloatVal: &f} }

// IntValue constructs a PropertyValue holding an int64.
func IntValue(i int64) PropertyValue { return PropertyValue{IntVal: &i} }

// BoolValue constructs a PropertyValue holding a bool.
func BoolValue(b bool) PropertyValue { return PropertyValue{BoolVal: &b} }

// StringValue constructs a PropertyValue holding a string.
func StringValue(s string) PropertyValue { return PropertyValue{StringVal: &s} }

// JSONValue constructs a PropertyValue holding a structured value encoded as JSON bytes.
func JSONValue(v any) (PropertyValue, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return PropertyValue{}, err
	}
	return PropertyValue{JSONVal: b}, nil
}
