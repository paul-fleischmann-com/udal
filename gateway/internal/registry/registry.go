// Package registry provides an in-memory and bbolt-backed device registry.
package registry

import (
	"fmt"
	"sync"
	"time"

	"github.com/paulefl/udal/gateway/internal/api"
)

// ErrNotFound is returned when a device does not exist in the registry.
var ErrNotFound = fmt.Errorf("device not found")

// ErrAlreadyExists is returned when registering a device with a duplicate ID.
var ErrAlreadyExists = fmt.Errorf("device already exists")

// Registry stores and retrieves Device records.
// Implementations must be safe for concurrent use.
type Registry interface {
	// Register adds a new device. If d.ID is empty, a unique ID is generated.
	Register(d api.Device) (api.Device, error)
	// Get returns the device with the given ID or ErrNotFound.
	Get(id string) (api.Device, error)
	// List returns all devices optionally filtered by capability and/or transport.
	// Empty strings mean "no filter".
	List(capability, transport string) ([]api.Device, error)
	// Delete removes a device. Returns ErrNotFound if the device does not exist.
	Delete(id string) error
	// UpdateStatus sets the device's status and last-seen timestamp.
	UpdateStatus(id string, status api.DeviceStatus, lastSeen time.Time) error
}

// MemoryRegistry is a thread-safe in-memory Registry implementation used for tests.
type MemoryRegistry struct {
	mu      sync.RWMutex
	devices map[string]api.Device
	nextID  int
}

// NewMemoryRegistry creates an empty in-memory registry.
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{devices: make(map[string]api.Device)}
}

func (r *MemoryRegistry) Register(d api.Device) (api.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if d.ID == "" {
		r.nextID++
		d.ID = fmt.Sprintf("dev-%05d", r.nextID)
	}
	if _, exists := r.devices[d.ID]; exists {
		return api.Device{}, fmt.Errorf("%w: %s", ErrAlreadyExists, d.ID)
	}
	if d.Labels == nil {
		d.Labels = make(map[string]string)
	}
	d.Status = api.DeviceStatusUnknown
	r.devices[d.ID] = d
	return d, nil
}

func (r *MemoryRegistry) Get(id string) (api.Device, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[id]
	if !ok {
		return api.Device{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return d, nil
}

func (r *MemoryRegistry) List(capability, transport string) ([]api.Device, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]api.Device, 0, len(r.devices))
	for _, d := range r.devices {
		if capability != "" && d.Capability != capability {
			continue
		}
		if transport != "" && d.Transport != transport {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func (r *MemoryRegistry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.devices[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(r.devices, id)
	return nil
}

func (r *MemoryRegistry) UpdateStatus(id string, status api.DeviceStatus, lastSeen time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	d.Status = status
	d.LastSeen = lastSeen
	r.devices[id] = d
	return nil
}
