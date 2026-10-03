// Package auth implements API key authentication for m3m0s serve — a
// Phase 1 item marked as critical in docs/implementation-plan.md because
// the previous memory-server had NO authentication at all, acceptable only
// while it was localhost-only. It never stores the raw key: only the SHA-256
// hash and a short prefix for display/debug (docs/api-contract.md, Auth
// section).
//
// Since D28 (docs/decisions/), keys are persisted in the SQLite api_keys
// table (internal/sqlstore) — this replaces the old dataDir/apikeys.json.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

const rawKeyAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// rawKeyLength is the size of the random part of the key (without the
// "m3m0s_" prefix).
const rawKeyLength = 40

// prefixDisplayLength is how many characters of the raw key we keep in clear
// text, only for display in listings (e.g. "m3m0s_aB3f" — never the whole key).
const prefixDisplayLength = 10

// Key is an API key record: it never contains the raw key, only the hash.
type Key struct {
	Prefix    string
	Hash      string
	Label     string
	CreatedAt time.Time
}

// GenerateRawKey generates a new key in the format "m3m0s_<40 random
// characters>". It is returned only once, at creation time — after that
// only the hash survives.
func GenerateRawKey() (string, error) {
	b := make([]byte, rawKeyLength)
	random := make([]byte, rawKeyLength)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	for i, r := range random {
		b[i] = rawKeyAlphabet[int(r)%len(rawKeyAlphabet)]
	}
	return "m3m0s_" + string(b), nil
}

func hashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func displayPrefix(rawKey string) string {
	if len(rawKey) <= prefixDisplayLength {
		return rawKey
	}
	return rawKey[:prefixDisplayLength]
}

// Store is the collection of valid API keys, persisted in SQLite (D28) — the
// in-memory cache (keys) avoids a database round trip on every Verify
// (called on every authenticated request), and is reloaded from the
// database only in LoadStore.
type Store struct {
	db *sqlstore.DB

	mu   sync.RWMutex
	keys []Key
}

// LoadStore reads the API keys already persisted in db (or starts empty if
// the table has no rows yet).
func LoadStore(db *sqlstore.DB) (*Store, error) {
	s := &Store{db: db}
	rows, err := db.ListAPIKeys(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load api keys: %w", err)
	}
	for _, r := range rows {
		s.keys = append(s.keys, Key{Prefix: r.Prefix, Hash: r.Hash, Label: r.Label, CreatedAt: r.CreatedAt})
	}
	return s, nil
}

// IsEmpty reports whether no API key has been registered yet.
func (s *Store) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.keys) == 0
}

// Count returns how many API keys are registered — used by GET /stats
// (telemetry) to report the number of access keys issued.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.keys)
}

// Create generates a new key, stores the hash and persists it. It returns
// the raw key (the only time it exists outside of the hash).
func (s *Store) Create(label string) (rawKey string, key Key, err error) {
	rawKey, err = GenerateRawKey()
	if err != nil {
		return "", Key{}, err
	}
	key = Key{
		Prefix:    displayPrefix(rawKey),
		Hash:      hashKey(rawKey),
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}

	if err := s.db.InsertAPIKey(context.Background(), sqlstore.APIKeyRow{
		Prefix: key.Prefix, Hash: key.Hash, Label: key.Label, CreatedAt: key.CreatedAt,
	}); err != nil {
		return "", Key{}, fmt.Errorf("persist api key: %w", err)
	}

	s.mu.Lock()
	s.keys = append(s.keys, key)
	s.mu.Unlock()
	return rawKey, key, nil
}

// Verify checks whether rawKey matches any registered key, using a
// constant-time comparison over the hash.
func (s *Store) Verify(rawKey string) bool {
	if rawKey == "" {
		return false
	}
	candidate := hashKey(rawKey)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.keys {
		if subtle.ConstantTimeCompare([]byte(k.Hash), []byte(candidate)) == 1 {
			return true
		}
	}
	return false
}

// BearerToken extracts the raw key from an "Authorization: Bearer <key>" header.
func BearerToken(authHeader string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
