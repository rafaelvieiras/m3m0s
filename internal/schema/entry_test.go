package schema

import (
	"strings"
	"testing"
	"time"
)

func TestValidateDomain(t *testing.T) {
	valid := []string{"code:m3m0s", "writing:practical-ai-book", "personal:social-media", "client:acme_corp"}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, want nil", d, err)
		}
	}

	invalid := []string{"", "code", "Code:m3m0s", "code:", ":m3m0s", "code m3m0s:x"}
	for _, d := range invalid {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) = nil, want error", d)
		}
	}
}

func TestDomainCategory(t *testing.T) {
	if got := DomainCategory("code:m3m0s"); got != "code" {
		t.Errorf("DomainCategory = %q, want code", got)
	}
}

func TestNewEntryDefaultsSourceAndRequiresContent(t *testing.T) {
	now := time.Now()
	entry, err := NewEntry("x", "code:m3m0s", "", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if entry.Source != DefaultSource {
		t.Errorf("Source = %q, want %q", entry.Source, DefaultSource)
	}
	if entry.Metadata == nil {
		t.Error("Metadata is nil when input metadata is nil — serializes as JSON `null`, breaking clients typed as object (e.g. the WebUI's Zod schema); should become map[string]string{}")
	}

	if _, err := NewEntry("   ", "code:m3m0s", "", "", nil, nil, now, ""); err == nil {
		t.Error("NewEntry with empty content should fail")
	}
}

func TestNewEntryIDPrefix(t *testing.T) {
	now := time.Now()

	entry, err := NewEntry("x", "code:m3m0s", "", "", nil, nil, now, "")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if !strings.HasPrefix(entry.ID, "mem_") {
		t.Errorf("ID = %q, want prefix mem_ when idPrefix is empty", entry.ID)
	}

	chunkEntry, err := NewEntry("x", "code:m3m0s", "watch", "", nil, nil, now, "chunk")
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if !strings.HasPrefix(chunkEntry.ID, "chunk_") {
		t.Errorf("ID = %q, want prefix chunk_ when idPrefix = \"chunk\"", chunkEntry.ID)
	}
}
