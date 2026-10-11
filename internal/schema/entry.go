// Package schema defines the public memory model (4 axes:
// domain/source/agent/tags+metadata, decision D9), as specified in
// docs/memory-schema.md. Since D28 (docs/decisions/), persistence is
// SQLite (internal/sqlstore) — Entry maps directly to the columns of the
// entries table (internal/store.entryToRow/rowToEntry), without going
// through a flattened map[string]string as in the old chromem-go.
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/idgen"
)

// domainPattern is the validation regex from docs/memory-schema.md and
// docs/api-contract.md: "<category>:<instance>".
var domainPattern = regexp.MustCompile(`^[a-z0-9_]+:[a-z0-9._-]+$`)

// projectPattern validates a project slug; it mirrors the instance part of
// a domain so a project can reuse a repository or folder name.
var projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// MaxModelLength bounds the free-form model identifier (provider slugs such
// as "anthropic/claude-sonnet-5-5" fit comfortably).
const MaxModelLength = 128

// ValidateProject checks a non-empty project slug.
func ValidateProject(project string) error {
	if !projectPattern.MatchString(project) {
		return fmt.Errorf("project must match %s", projectPattern.String())
	}
	return nil
}

// ValidateModel checks a non-empty model identifier: printable, no
// whitespace, bounded length.
func ValidateModel(model string) error {
	if len(model) > MaxModelLength || strings.ContainsAny(model, " \t\r\n") {
		return fmt.Errorf("model must be at most %d characters with no whitespace", MaxModelLength)
	}
	return nil
}

// DefaultSource is used when the call does not provide "source".
const DefaultSource = "manual"

// Entry is the public model of a memory, used on all 3 surfaces
// (HTTP/MCP/CLI) per docs/api-contract.md.
type Entry struct {
	ID             string  `json:"id"`
	Content        string  `json:"content"`
	Domain         string  `json:"domain"`
	DomainCategory string  `json:"domainCategory"`
	Source         string  `json:"source"`
	Agent          *string `json:"agent"`
	// Model is the LLM that produced the memory (provenance, F2); nil when
	// unknown, as for every entry written before the field existed.
	Model *string `json:"model"`
	// Project scopes the memory to one project; nil means global (the
	// default, and what every pre-F2 entry is). See docs/memory-schema.md.
	Project  *string           `json:"project"`
	Tags     []string          `json:"tags"`
	Metadata map[string]string `json:"metadata"`
	Score    *float64          `json:"score,omitempty"`
	// RelatedVia is filled only by the 1-hop expansion of search/recent
	// (Phase 7, D15 in docs/decisions/) — it points to the ID of the main
	// result that brought this entry in through a relation
	// (metadata.related_to). Never persisted (outside
	// ToMetadata/FromMetadata, same pattern as Score).
	RelatedVia *string   `json:"relatedVia,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ValidateDomain checks the domain against the pattern in docs/memory-schema.md.
func ValidateDomain(domain string) error {
	if !domainPattern.MatchString(domain) {
		return fmt.Errorf("domain must match %s", domainPattern.String())
	}
	return nil
}

// DomainCategory extracts the category (text before ":") from an already
// validated domain.
func DomainCategory(domain string) string {
	idx := strings.IndexByte(domain, ':')
	if idx < 0 {
		return domain
	}
	return domain[:idx]
}

// DefaultIDPrefix is the prefix used when NewEntry does not receive an
// explicit idPrefix — memories created via the normal add/dedup flow.
const DefaultIDPrefix = "mem"

// NewEntry builds an Entry from the input fields of an `add`, applying
// defaults and generating the ID/timestamps. It does no secret redaction
// or dedup — that is the store package's responsibility (add pipeline).
// An empty idPrefix falls back to the default "mem"; `m3m0s watch`
// (Phase 2, D11) asks for "chunk" so it is clear from reading the ID that
// the memory came from a reindexed file chunk, not from a direct `add`.
func NewEntry(content, domain, source, agent string, tags []string, metadata map[string]string, now time.Time, idPrefix string) (Entry, error) {
	if strings.TrimSpace(content) == "" {
		return Entry{}, errors.New("content is required")
	}
	if err := ValidateDomain(domain); err != nil {
		return Entry{}, err
	}
	if source == "" {
		source = DefaultSource
	}
	if idPrefix == "" {
		idPrefix = DefaultIDPrefix
	}
	if metadata == nil {
		// nil serializes as JSON `null`, not `{}` — it breaks clients that
		// type metadata as a required object (e.g. the WebUI's Zod schema).
		// FromMetadata (below) already normalizes this on re-read from
		// chromem-go; here we guarantee the same at creation, so an Entry
		// never comes out with nil Metadata while it still lives only in
		// the in-memory index (before any reload from disk).
		metadata = map[string]string{}
	}

	e := Entry{
		ID:             idgen.New(idPrefix),
		Content:        content,
		Domain:         domain,
		DomainCategory: DomainCategory(domain),
		Source:         source,
		Tags:           normalizeTags(tags),
		Metadata:       metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if agent != "" {
		e.Agent = &agent
	}
	return e, nil
}

// NormalizeTags exposes normalizeTags for callers that need to normalize a
// new set of tags outside the NewEntry flow — today only
// internal/store.Update (memory_update, D13/docs/api-contract.md), which
// replaces Entry.Tags entirely when the "tags" field is present in the
// PATCH.
func NormalizeTags(tags []string) []string {
	return normalizeTags(tags)
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// ModelValue returns Model, or an empty string when unknown.
func (e Entry) ModelValue() string {
	if e.Model == nil {
		return ""
	}
	return *e.Model
}

// ProjectValue returns Project, or an empty string when the entry is global.
func (e Entry) ProjectValue() string {
	if e.Project == nil {
		return ""
	}
	return *e.Project
}

// AgentValue returns the value of Agent, or an empty string when absent —
// a convenience for filters/CLI that do not work with *string.
func (e Entry) AgentValue() string {
	if e.Agent == nil {
		return ""
	}
	return *e.Agent
}

// KindValue returns the nature of the fact (D38): the metadata value when it
// is a valid kind, otherwise "other" — which is also what every row written
// before the feature reads as.
func (e Entry) KindValue() string {
	if k := e.Metadata[factkind.MetadataKey]; factkind.Valid(k) {
		return k
	}
	return factkind.Other
}

// KindSourceValue returns how the kind was set ("llm", "heuristic" or
// "manual"), or an empty string when the entry was never classified.
func (e Entry) KindSourceValue() string {
	if src := e.Metadata[factkind.MetadataSourceKey]; factkind.ValidSource(src) {
		return src
	}
	return ""
}

// MarshalJSON adds the derived top-level `kind` and `kindSource` fields to the
// public representation; the stored form stays in Metadata only.
func (e Entry) MarshalJSON() ([]byte, error) {
	type plain Entry
	return json.Marshal(struct {
		plain
		Kind       string `json:"kind"`
		KindSource string `json:"kindSource,omitempty"`
	}{plain(e), e.KindValue(), e.KindSourceValue()})
}

// HasTag checks whether the entry has the given tag (exact comparison).
func (e Entry) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
