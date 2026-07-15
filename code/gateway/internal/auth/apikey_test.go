package auth_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/auth"
	"go.etcd.io/bbolt"
)

func newAPIKeyStore(t *testing.T) *auth.APIKeyStore {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "keys.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open bbolt db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := auth.NewAPIKeyStore(db)
	if err != nil {
		t.Fatalf("NewAPIKeyStore: %v", err)
	}
	return store
}

func TestAPIKeyStore_AuthenticateValidKey(t *testing.T) {
	store := newAPIKeyStore(t)
	if err := store.Put("alice", auth.RoleAdmin, "s3cret-key"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	id, err := store.Authenticate("s3cret-key")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Subject != "alice" || id.Role != auth.RoleAdmin {
		t.Errorf("Identity = %+v, want subject=alice role=admin", id)
	}
}

func TestAPIKeyStore_UnknownKeyRejected(t *testing.T) {
	store := newAPIKeyStore(t)
	if err := store.Put("alice", auth.RoleAdmin, "s3cret-key"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, err := store.Authenticate("wrong-key")
	if !errors.Is(err, auth.ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestAPIKeyStore_Rotation(t *testing.T) {
	store := newAPIKeyStore(t)
	if err := store.Put("alice", auth.RoleAdmin, "old-key"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Rotate: same subject, new key.
	if err := store.Put("alice", auth.RoleAdmin, "new-key"); err != nil {
		t.Fatalf("Put (rotate): %v", err)
	}

	if _, err := store.Authenticate("new-key"); err != nil {
		t.Errorf("new key should be accepted immediately: %v", err)
	}
	if _, err := store.Authenticate("old-key"); !errors.Is(err, auth.ErrKeyNotFound) {
		t.Errorf("old key should be rejected after rotation, got %v", err)
	}
}

func TestAPIKeyStore_Has(t *testing.T) {
	store := newAPIKeyStore(t)
	if has, _ := store.Has("alice"); has {
		t.Error("expected Has(alice) = false before Put")
	}
	if err := store.Put("alice", auth.RoleAdmin, "key"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if has, _ := store.Has("alice"); !has {
		t.Error("expected Has(alice) = true after Put")
	}
}
