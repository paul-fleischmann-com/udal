package auth

import (
	"encoding/json"
	"errors"
	"fmt"

	"go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost matches F-16's "bcrypt hashes (cost >= 12)" requirement.
const bcryptCost = 12

var apiKeysBucket = []byte("api_keys")

// ErrKeyNotFound is returned when an API key subject has no stored record.
var ErrKeyNotFound = errors.New("api key not found")

// apiKeyRecord is the persisted form of a provisioned API key. The raw key
// is never stored, only its bcrypt hash (F-16: "Keys are never returned in
// API responses").
type apiKeyRecord struct {
	Subject   string
	Role      Role
	HashedKey []byte
}

// APIKeyStore persists API key records in their own bbolt bucket — sharing
// the device registry's database file (see registry.BboltRegistry.DB) rather
// than a separate one, per F-16 ("stored ... in Device Registry").
type APIKeyStore struct {
	db *bbolt.DB
}

// NewAPIKeyStore opens (creating if necessary) the api_keys bucket in db.
func NewAPIKeyStore(db *bbolt.DB) (*APIKeyStore, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(apiKeysBucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create api_keys bucket: %w", err)
	}
	return &APIKeyStore{db: db}, nil
}

// Put hashes rawKey and stores (or replaces) the record for subject. Used
// both for initial provisioning and for key rotation (F-16: "new key
// accepted immediately; old key rejected after rotation" — rotation is just
// calling Put again with a new rawKey, overwriting the old hash).
func (s *APIKeyStore) Put(subject string, role Role, rawKey string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash api key: %w", err)
	}
	rec := apiKeyRecord{Subject: subject, Role: role, HashedKey: hashed}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal api key record: %w", err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(apiKeysBucket).Put([]byte(subject), data)
	})
}

// Has reports whether a key is already provisioned for subject, without
// revealing anything about it — used by the bootstrap step in main.go to
// stay idempotent across restarts.
func (s *APIKeyStore) Has(subject string) (bool, error) {
	var found bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		found = tx.Bucket(apiKeysBucket).Get([]byte(subject)) != nil
		return nil
	})
	return found, err
}

// Authenticate looks up every stored key and returns the Identity of the one
// whose hash matches rawKey. Returns ErrKeyNotFound if none match.
//
// Subjects aren't looked up by rawKey directly (bcrypt hashes aren't
// deterministic, so there's no index to look up by), hence the scan — fine
// at the expected scale of provisioned API keys (operators/services, not
// per-device credentials, which use mTLS instead).
func (s *APIKeyStore) Authenticate(rawKey string) (Identity, error) {
	var match *apiKeyRecord
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(apiKeysBucket).ForEach(func(_, data []byte) error {
			if match != nil {
				return nil
			}
			var rec apiKeyRecord
			if err := json.Unmarshal(data, &rec); err != nil {
				return fmt.Errorf("unmarshal api key record: %w", err)
			}
			if bcrypt.CompareHashAndPassword(rec.HashedKey, []byte(rawKey)) == nil {
				match = &rec
			}
			return nil
		})
	})
	if err != nil {
		return Identity{}, err
	}
	if match == nil {
		return Identity{}, ErrKeyNotFound
	}
	return Identity{Subject: match.Subject, Role: match.Role}, nil
}
