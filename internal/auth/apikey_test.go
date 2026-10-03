package auth

import (
	"testing"

	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

func openTestDB(t *testing.T) *sqlstore.DB {
	t.Helper()
	db, err := sqlstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("sqlstore.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGenerateRawKeyFormat(t *testing.T) {
	raw, err := GenerateRawKey()
	if err != nil {
		t.Fatalf("GenerateRawKey: %v", err)
	}
	if len(raw) != len("m3m0s_")+rawKeyLength {
		t.Fatalf("unexpected length: %q (%d chars)", raw, len(raw))
	}
	if raw[:6] != "m3m0s_" {
		t.Fatalf("unexpected prefix: %q", raw)
	}
}

func TestStoreCreateAndVerify(t *testing.T) {
	s, err := LoadStore(openTestDB(t))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if !s.IsEmpty() {
		t.Fatal("newly created store should be empty")
	}

	raw, key, err := s.Create("test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key.Hash == "" || key.Hash == raw {
		t.Fatalf("invalid hash or equal to raw key: %q", key.Hash)
	}
	if s.IsEmpty() {
		t.Fatal("store should not be empty after Create")
	}
	if !s.Verify(raw) {
		t.Fatal("Verify should accept the newly created key")
	}
	if s.Verify("m3m0s_wrong-key") {
		t.Fatal("Verify should not accept a nonexistent key")
	}
	if s.Verify("") {
		t.Fatal("Verify should not accept an empty key")
	}
}

func TestStoreCount(t *testing.T) {
	s, err := LoadStore(openTestDB(t))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if got := s.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}

	if _, _, err := s.Create("one"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := s.Create("two"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := s.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}
}

func TestStorePersistsAcrossLoad(t *testing.T) {
	db := openTestDB(t)

	s1, err := LoadStore(db)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	raw, _, err := s1.Create("persistent")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s2, err := LoadStore(db)
	if err != nil {
		t.Fatalf("second LoadStore: %v", err)
	}
	if s2.IsEmpty() {
		t.Fatal("second Store should have loaded the persisted key")
	}
	if !s2.Verify(raw) {
		t.Fatal("second Store should verify the key created by the first")
	}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header    string
		wantToken string
		wantOK    bool
	}{
		{"Bearer m3m0s_abc", "m3m0s_abc", true},
		{"Bearer   m3m0s_abc  ", "m3m0s_abc", true},
		{"", "", false},
		{"Basic dXNlcjpwYXNz", "", false},
		{"Bearer ", "", false},
	}
	for _, c := range cases {
		token, ok := BearerToken(c.header)
		if ok != c.wantOK || token != c.wantToken {
			t.Errorf("BearerToken(%q) = (%q, %v), want (%q, %v)", c.header, token, ok, c.wantToken, c.wantOK)
		}
	}
}
