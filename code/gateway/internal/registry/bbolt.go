package registry

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
	"go.etcd.io/bbolt"
)

var devicesBucket = []byte("devices")

// BboltRegistry is a Registry implementation persisted to an embedded bbolt
// database file. It is safe for concurrent use: all reads/writes go through
// bbolt transactions, which serialize writers and allow concurrent readers.
type BboltRegistry struct {
	db *bbolt.DB
}

// NewBboltRegistry opens (creating if necessary) a bbolt database at path and
// returns a Registry backed by it. Callers must call Close when done.
func NewBboltRegistry(path string) (*BboltRegistry, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt db %q: %w", path, err)
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(devicesBucket)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create devices bucket: %w", err)
	}
	return &BboltRegistry{db: db}, nil
}

// DB returns the underlying bbolt database handle, so other packages (e.g.
// internal/auth's API-key store) can persist their own data in the same
// database file without opening a second file.
func (r *BboltRegistry) DB() *bbolt.DB { return r.db }

// Close closes the underlying database file.
func (r *BboltRegistry) Close() error {
	return r.db.Close()
}

func (r *BboltRegistry) Register(d api.Device) (api.Device, error) {
	err := r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(devicesBucket)
		if d.ID == "" {
			seq, err := b.NextSequence()
			if err != nil {
				return fmt.Errorf("generate id: %w", err)
			}
			d.ID = fmt.Sprintf("dev-%05d", seq)
		}
		if b.Get([]byte(d.ID)) != nil {
			return fmt.Errorf("%w: %s", ErrAlreadyExists, d.ID)
		}
		if d.Labels == nil {
			d.Labels = make(map[string]string)
		}
		d.Status = api.DeviceStatusUnknown
		data, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshal device: %w", err)
		}
		return b.Put([]byte(d.ID), data)
	})
	if err != nil {
		return api.Device{}, err
	}
	return d, nil
}

func (r *BboltRegistry) Get(id string) (api.Device, error) {
	var d api.Device
	err := r.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(devicesBucket).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return json.Unmarshal(data, &d)
	})
	if err != nil {
		return api.Device{}, err
	}
	return d, nil
}

func (r *BboltRegistry) List(filter ListFilter) ([]api.Device, error) {
	out := []api.Device{}
	err := r.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(devicesBucket).ForEach(func(_, data []byte) error {
			var d api.Device
			if err := json.Unmarshal(data, &d); err != nil {
				return fmt.Errorf("unmarshal device: %w", err)
			}
			if matchesFilter(d, filter) {
				out = append(out, d)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *BboltRegistry) Delete(id string) error {
	return r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(devicesBucket)
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return b.Delete([]byte(id))
	})
}

func (r *BboltRegistry) UpdateStatus(id string, status api.DeviceStatus, lastSeen time.Time) error {
	return r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(devicesBucket)
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		var d api.Device
		if err := json.Unmarshal(data, &d); err != nil {
			return fmt.Errorf("unmarshal device: %w", err)
		}
		d.Status = status
		d.LastSeen = lastSeen
		newData, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshal device: %w", err)
		}
		return b.Put([]byte(id), newData)
	})
}

func (r *BboltRegistry) UpdateACL(id string, acl []api.ACLEntry) error {
	return r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(devicesBucket)
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		var d api.Device
		if err := json.Unmarshal(data, &d); err != nil {
			return fmt.Errorf("unmarshal device: %w", err)
		}
		d.ACL = acl
		newData, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshal device: %w", err)
		}
		return b.Put([]byte(id), newData)
	})
}
