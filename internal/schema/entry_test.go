package schema

import (
	"encoding/json"
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

func TestEntryKindAccessorsAndJSON(t *testing.T) {
	legacy := Entry{ID: "mem_1", Metadata: map[string]string{}}
	if legacy.KindValue() != "other" || legacy.KindSourceValue() != "" {
		t.Errorf("legacy reads %q/%q", legacy.KindValue(), legacy.KindSourceValue())
	}
	bad := Entry{Metadata: map[string]string{"kind": "bogus", "kind_source": "auto"}}
	if bad.KindValue() != "other" || bad.KindSourceValue() != "" {
		t.Errorf("invalid values must read as unclassified: %q/%q", bad.KindValue(), bad.KindSourceValue())
	}

	tagged := Entry{ID: "mem_2", Metadata: map[string]string{"kind": "rule", "kind_source": "manual"}}
	b, err := json.Marshal(tagged)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out["kind"] != "rule" || out["kindSource"] != "manual" || out["id"] != "mem_2" {
		t.Errorf("json = %s", b)
	}
	if _, has := out["metadata"].(map[string]any)["kind"]; !has {
		t.Error("the stored form stays in metadata too")
	}

	b, _ = json.Marshal(legacy)
	out = map[string]any{}
	_ = json.Unmarshal(b, &out)
	if out["kind"] != "other" {
		t.Errorf("legacy json kind = %v", out["kind"])
	}
	if _, has := out["kindSource"]; has {
		t.Error("kindSource must be omitted when never classified")
	}
	// round trip through the public JSON keeps the entry intact
	var back Entry
	if err := json.Unmarshal(b, &back); err != nil || back.ID != "mem_1" {
		t.Errorf("round trip: %v %+v", err, back)
	}
}
