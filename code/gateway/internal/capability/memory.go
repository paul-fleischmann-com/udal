package capability

import (
	"fmt"
	"sync"
	"time"
)

// MemoryRegistry is a thread-safe in-memory Registry, used for tests.
type MemoryRegistry struct {
	opts options

	mu      sync.Mutex
	schemas map[string]Schema // key: name@version
}

// NewMemoryRegistry returns an empty, thread-safe in-memory Registry.
func NewMemoryRegistry(opts ...Option) *MemoryRegistry {
	return &MemoryRegistry{opts: newOptions(opts), schemas: make(map[string]Schema)}
}

func (r *MemoryRegistry) Publish(raw []byte) (Schema, error) {
	s, err := validateAndParse(raw)
	if err != nil {
		return Schema{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := publishKey(s.Name, s.Version)
	if _, exists := r.schemas[key]; exists {
		return Schema{}, fmt.Errorf("%w: %s", ErrAlreadyExists, key)
	}

	if latest, ok := r.latestLocked(s.Name); ok {
		warnIfBreaking(r.opts.log, latest, s)
	}

	s.PublishedAt = time.Now()
	r.schemas[key] = s
	return s, nil
}

// latestLocked returns the highest-versioned existing schema for name, if
// any. Callers must hold r.mu.
func (r *MemoryRegistry) latestLocked(name string) (Schema, bool) {
	var latest Schema
	var latestVer semver
	found := false
	for _, existing := range r.schemas {
		if existing.Name != name {
			continue
		}
		v, err := parseSemver(existing.Version)
		if err != nil {
			continue
		}
		if !found || latestVer.less(v) {
			latest, latestVer, found = existing, v, true
		}
	}
	return latest, found
}

func (r *MemoryRegistry) Get(name, version string) (Schema, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.schemas[publishKey(name, version)]
	if !ok {
		return Schema{}, fmt.Errorf("%w: %s", ErrNotFound, publishKey(name, version))
	}
	return s, nil
}

func (r *MemoryRegistry) List(name string) ([]Schema, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Schema, 0, len(r.schemas))
	for _, s := range r.schemas {
		if name != "" && s.Name != name {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
