package apitypes

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

func TestBuildContextBlockShowsProvenanceForWatchChunks(t *testing.T) {
	score := 0.73
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	entries := []schema.Entry{
		{
			Domain: "codigo:m3m0s", Source: "watch", Content: "chunk excerpt",
			CreatedAt: now, Score: &score,
			Metadata: map[string]string{"file_path": "AGENTS.md", "heading": "Decisions already made"},
		},
	}

	block := BuildContextBlock(entries)
	if !strings.Contains(block, "— AGENTS.md § Decisions already made") {
		t.Errorf("contextBlock should show file_path/heading, got: %s", block)
	}
}

func TestBuildContextBlockOmitsProvenanceWhenAbsent(t *testing.T) {
	entries := []schema.Entry{
		{Domain: "codigo:m3m0s", Source: "manual", Content: "plain memory", CreatedAt: time.Now(), Metadata: map[string]string{}},
	}

	block := BuildContextBlock(entries)
	if strings.Contains(block, "—") || strings.Contains(block, "§") {
		t.Errorf("contextBlock should not have a provenance suffix without file_path, got: %s", block)
	}
}

// TestBuildContextBlockMarksExpandedEntriesInsteadOfNumbering covers Phase
// 7 (D15): entries brought in by 1-hop expansion (non-nil RelatedVia) do not
// take part in the normal numbering — they appear as "↳ related to <id>" and
// do not increment the counter of the next result.
func TestBuildContextBlockMarksExpandedEntriesInsteadOfNumbering(t *testing.T) {
	score1 := 0.9
	score2 := 0.8
	via := "mem_primary"
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	entries := []schema.Entry{
		{ID: "mem_primary", Domain: "codigo:m3m0s", Source: "manual", Content: "primary result", CreatedAt: now, Score: &score1, Metadata: map[string]string{}},
		{ID: "mem_related", Domain: "codigo:m3m0s", Source: "manual", Content: "expanded result", CreatedAt: now, RelatedVia: &via, Metadata: map[string]string{}},
		{ID: "mem_second", Domain: "codigo:m3m0s", Source: "manual", Content: "second query result", CreatedAt: now, Score: &score2, Metadata: map[string]string{}},
	}

	block := BuildContextBlock(entries)
	if !strings.Contains(block, "↳ related to mem_primary") {
		t.Errorf("expected expansion marker, got: %s", block)
	}
	if !strings.Contains(block, "1. [0.90]") || !strings.Contains(block, "2. [0.80]") {
		t.Errorf("expected numbering 1/2 only for the direct query results (the expanded one should not count), got: %s", block)
	}
	if strings.Contains(block, "3. [") {
		t.Errorf("expanded entry should not get number 3, got: %s", block)
	}
}

func TestBuildContextBlockEmpty(t *testing.T) {
	block := BuildContextBlock(nil)
	if !strings.Contains(block, "no relevant memories") {
		t.Errorf("empty contextBlock should say nothing was found, got: %s", block)
	}
}

// TestAddResponseSerializesAsPluralShape checks the new Phase 4 shape
// (D12, item 2, breaking): "entries" (array) + "extracted", no longer the
// singular "entry" of Phase 1/2/3.
func TestAddResponseSerializesAsPluralShape(t *testing.T) {
	resp := AddResponse{
		Entries: []AddedEntry{
			{Entry: schema.Entry{ID: "mem_1", Content: "fact 1"}, Deduped: false},
			{Entry: schema.Entry{ID: "mem_2", Content: "fact 2"}, Deduped: true},
		},
		Extracted: true,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["entry"]; ok {
		t.Error("should not have the singular \"entry\" key (old shape, pre-Phase-4)")
	}
	entries, ok := decoded["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("expected \"entries\" with 2 items, got: %s", b)
	}
	if decoded["extracted"] != true {
		t.Errorf("expected \"extracted\":true, got: %s", b)
	}

	var roundTrip AddResponse
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if len(roundTrip.Entries) != 2 || roundTrip.Entries[1].Deduped != true {
		t.Fatalf("round-trip did not preserve the data: %#v", roundTrip)
	}
}

// ── StatsResponse.Extraction (Phase 5, D13) ──

func TestStatsResponseExtractionEnabledSerializesModel(t *testing.T) {
	model := "deepseek/deepseek-v4-flash"
	resp := StatsResponse{
		Count: 1, CountByDomainCategory: map[string]int{"codigo": 1}, Backend: "b", Model: "m",
		Extraction: ExtractionInfo{Enabled: true, Model: &model},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	extraction, ok := decoded["extraction"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"extraction\" field in the JSON, got: %s", b)
	}
	if extraction["enabled"] != true {
		t.Errorf("expected enabled:true, got: %s", b)
	}
	if extraction["model"] != model {
		t.Errorf("expected model=%q, got %v", model, extraction["model"])
	}
}

// TestStatsResponseExtractionDisabledModelIsNullNeverEmptyString checks the
// explicit contract rule: enabled:false => model:null, never "" — the
// consumer should not need to check both fields to know extraction is off.
func TestStatsResponseExtractionDisabledModelIsNullNeverEmptyString(t *testing.T) {
	resp := StatsResponse{
		Count: 0, CountByDomainCategory: map[string]int{}, Backend: "b", Model: "m",
		Extraction: ExtractionInfo{Enabled: false, Model: nil},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	extraction := decoded["extraction"].(map[string]any)
	if extraction["enabled"] != false {
		t.Errorf("expected enabled:false, got: %s", b)
	}
	if v, ok := extraction["model"]; !ok || v != nil {
		t.Errorf("expected model:null (key present, nil value), got %v (%s)", v, b)
	}

	var roundTrip StatsResponse
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if roundTrip.Extraction.Model != nil {
		t.Errorf("round-trip: expected nil Model, got %v", *roundTrip.Extraction.Model)
	}
}

// ── StatsResponse.Telemetry ──

func TestStatsResponseTelemetryRoundTrips(t *testing.T) {
	resp := StatsResponse{
		Count: 3, CountByDomainCategory: map[string]int{"codigo": 3}, Backend: "b", Model: "m",
		Extraction: ExtractionInfo{Enabled: false, Model: nil},
		Telemetry: TelemetryInfo{
			APIKeyCount:             2,
			MemoryUsageMB:           12.5,
			DatabaseSizeMB:          0.75,
			UptimeSeconds:           3600,
			ExtractionTokensUsed:    1500,
			TokensSavedByCompaction: 4200,
		},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	telemetry, ok := decoded["telemetry"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"telemetry\" field in the JSON, got: %s", b)
	}
	if telemetry["apiKeyCount"] != float64(2) {
		t.Errorf("unexpected apiKeyCount: %v", telemetry["apiKeyCount"])
	}
	if telemetry["tokensSavedByCompaction"] != float64(4200) {
		t.Errorf("unexpected tokensSavedByCompaction: %v", telemetry["tokensSavedByCompaction"])
	}

	var roundTrip StatsResponse
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if roundTrip.Telemetry != resp.Telemetry {
		t.Fatalf("round-trip did not preserve Telemetry: got %#v, want %#v", roundTrip.Telemetry, resp.Telemetry)
	}
}

// ── UpdateRequest (memory_update, D13) ──

// TestUpdateRequestHasAnyFieldDistinguishesAbsentFromPresent checks that
// json.RawMessage preserves the distinction "key absent" vs. "key present
// with a null/empty value" — the central mechanism docs/api-contract.md
// requires for memory_update, since decoding straight into simple pointers
// (*string etc.) would not preserve that distinction.
func TestUpdateRequestHasAnyFieldDistinguishesAbsentFromPresent(t *testing.T) {
	var empty UpdateRequest
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if empty.HasAnyField() {
		t.Error("an empty body should have no field present")
	}

	var withNullAgent UpdateRequest
	if err := json.Unmarshal([]byte(`{"agent":null}`), &withNullAgent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !withNullAgent.HasAnyField() {
		t.Error("\"agent\":null should count as a present field (clears the agent), not absent")
	}
	if len(withNullAgent.Agent) == 0 {
		t.Error("Agent should carry the literal bytes of null (len > 0), not be empty")
	}

	var withEmptyTags UpdateRequest
	if err := json.Unmarshal([]byte(`{"tags":[]}`), &withEmptyTags); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !withEmptyTags.HasAnyField() {
		t.Error("\"tags\":[] should count as a present field (clears the tags)")
	}

	var withContent UpdateRequest
	if err := json.Unmarshal([]byte(`{"content":"new text"}`), &withContent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !withContent.HasAnyField() {
		t.Error("present content should count as a present field")
	}
	if withContent.Domain != nil {
		t.Error("absent domain should not have been populated")
	}
}

// TestUpdateRequestMarshalOmitsAbsentFields checks the reverse path: the CLI
// builds an UpdateRequest by only setting RawMessage on the fields whose
// flag was passed — omitempty must really exclude the absent fields from
// the JSON sent, not serialize "null" for them.
func TestUpdateRequestMarshalOmitsAbsentFields(t *testing.T) {
	req := UpdateRequest{Content: json.RawMessage(`"new content"`)}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected only the \"content\" key in the JSON, got: %s", b)
	}
	if _, ok := decoded["content"]; !ok {
		t.Errorf("expected the \"content\" key to be present, got: %s", b)
	}
}

// TestUpdateResponseIsPlainEntryNotEnveloped checks that UpdateResponse is a
// direct alias of schema.Entry (not enveloped in "entries"/"entry") — the
// contract is explicit that memory_update always acts on exactly 1 entry.
func TestUpdateResponseIsPlainEntryNotEnveloped(t *testing.T) {
	resp := UpdateResponse(schema.Entry{ID: "mem_1", Content: "updated", Domain: "codigo:m3m0s"})

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["entries"]; ok {
		t.Error("UpdateResponse should not be enveloped in \"entries\"")
	}
	if decoded["id"] != "mem_1" {
		t.Errorf("expected id=mem_1 at the object's root level, got: %s", b)
	}
}

func TestFitTokenBudgetKeepsRankedPrefix(t *testing.T) {
	mk := func(n int) schema.Entry { return schema.Entry{Content: strings.Repeat("x", n)} }
	entries := []schema.Entry{mk(100), mk(100), mk(100)}
	per := EntryTokens(entries[0]) // 25 + overhead
	if got := FitTokenBudget(entries, 0); len(got) != 3 {
		t.Errorf("no cap: %d entries, want 3", len(got))
	}
	if got := FitTokenBudget(entries, per*2); len(got) != 2 {
		t.Errorf("budget for two: %d entries, want 2", len(got))
	}
	if got := FitTokenBudget(entries, per-1); len(got) != 0 {
		t.Errorf("budget below one entry: %d entries, want 0", len(got))
	}
}

func TestEntryTokensCapsAtRenderedSnippet(t *testing.T) {
	long := schema.Entry{Content: strings.Repeat("x", 5000)}
	if got := EntryTokens(long); got > 90 {
		t.Errorf("long entry costs %d tokens, but only a 240-char snippet is rendered", got)
	}
}
