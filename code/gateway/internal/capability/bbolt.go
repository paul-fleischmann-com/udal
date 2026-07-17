package capability

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

var capabilityBucket = []byte("capability_schemas")

// storedSchema is the bbolt storage envelope: the raw published document
// plus PublishedAt, which isn't part of the document itself and would
// otherwise be lost across a restart (Publish always sets it to time.Now()
// right before storing).
type storedSchema struct {
	PublishedAt time.Time       `json:"published_at"`
	Raw         json.RawMessage `json:"raw"`
}

// BboltRegistry is a Registry persisted to an embedded bbolt database.
// Callers share the same *bbolt.DB the device Registry (and API-key store)
// already opened — see registry.BboltRegistry.DB() — rather than a second
// database file, the same pattern auth.NewAPIKeyStore already uses.
type BboltRegistry struct {
	opts options
	db   *bbolt.DB
}

// NewBboltRegistry opens (creating if necessary) the capability_schemas
// bucket in db and returns a Registry backed by it.
func NewBboltRegistry(db *bbolt.DB, opts ...Option) (*BboltRegistry, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(capabilityBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("capability: create capability_schemas bucket: %w", err)
	}
	return &BboltRegistry{opts: newOptions(opts), db: db}, nil
}

// Only the raw published document (plus the storedSchema envelope's
// PublishedAt) is persisted; Get/List re-Parse the document, so there's
// exactly one source of truth for a schema's parsed shape (the same bytes
// that were validated at Publish time) rather than a second,
// independently-serialized copy of the derived fields that could drift.
func (r *BboltRegistry) Publish(raw []byte) (Schema, error) {
	s, err := validateAndParse(raw)
	if err != nil {
		return Schema{}, err
	}
	s.PublishedAt = time.Now()

	stored, err := json.Marshal(storedSchema{PublishedAt: s.PublishedAt, Raw: raw})
	if err != nil {
		return Schema{}, fmt.Errorf("capability: encode stored schema: %w", err)
	}

	key := []byte(publishKey(s.Name, s.Version))
	err = r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(capabilityBucket)
		if b.Get(key) != nil {
			return fmt.Errorf("%w: %s", ErrAlreadyExists, key)
		}

		if latest, ok, err := r.latestInTx(b, s.Name); err != nil {
			return err
		} else if ok {
			warnIfBreaking(r.opts.log, latest, s)
		}

		return b.Put(key, stored)
	})
	if err != nil {
		return Schema{}, err
	}
	return s, nil
}

func decodeStoredSchema(k, v []byte) (Schema, error) {
	var stored storedSchema
	if err := json.Unmarshal(v, &stored); err != nil {
		return Schema{}, fmt.Errorf("capability: corrupt stored schema %s: %w", k, err)
	}
	s, err := Parse(stored.Raw)
	if err != nil {
		return Schema{}, fmt.Errorf("capability: corrupt stored schema %s: %w", k, err)
	}
	s.PublishedAt = stored.PublishedAt
	return s, nil
}

func (r *BboltRegistry) latestInTx(b *bbolt.Bucket, name string) (Schema, bool, error) {
	var latest Schema
	var latestVer semver
	found := false
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		existing, err := decodeStoredSchema(k, v)
		if err != nil {
			return Schema{}, false, err
		}
		if existing.Name != name {
			continue
		}
		ver, err := parseSemver(existing.Version)
		if err != nil {
			continue
		}
		if !found || latestVer.less(ver) {
			latest, latestVer, found = existing, ver, true
		}
	}
	return latest, found, nil
}

func (r *BboltRegistry) Get(name, version string) (Schema, error) {
	var s Schema
	err := r.db.View(func(tx *bbolt.Tx) error {
		key := []byte(publishKey(name, version))
		data := tx.Bucket(capabilityBucket).Get(key)
		if data == nil {
			return fmt.Errorf("%w: %s", ErrNotFound, publishKey(name, version))
		}
		parsed, err := decodeStoredSchema(key, data)
		if err != nil {
			return err
		}
		s = parsed
		return nil
	})
	if err != nil {
		return Schema{}, err
	}
	return s, nil
}

func (r *BboltRegistry) List(name string) ([]Schema, error) {
	var out []Schema
	err := r.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(capabilityBucket).ForEach(func(k, v []byte) error {
			s, err := decodeStoredSchema(k, v)
			if err != nil {
				return err
			}
			if name != "" && s.Name != name {
				return nil
			}
			out = append(out, s)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
