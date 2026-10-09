package apitypes

import "time"

// Memory health ("Insights", D37). The overview is computed on request from
// the in-memory index; the hygiene report is produced by a manual/scheduled
// job and read back from the database. Neither ever contains embeddings,
// full secret-bearing text, or more than ~160 characters of any memory.

// CountItem is one bucket of a distribution.
type CountItem struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// DomainStat is one row of the top-domains table.
type DomainStat struct {
	Domain        string `json:"domain"`
	Count         int    `json:"count"`
	NeverAccessed int    `json:"never_accessed"`
	Expired       int    `json:"expired"`
}

// InsightsTotals are the headline counters. Everything except Total and
// SoftDeleted counts active (not soft-deleted) memories.
type InsightsTotals struct {
	Total            int     `json:"total"`
	Active           int     `json:"active"`
	SoftDeleted      int     `json:"soft_deleted"`
	WithValidUntil   int     `json:"with_valid_until"`
	Expired          int     `json:"expired"`
	Pinned           int     `json:"pinned"`
	Extracted        int     `json:"extracted"`
	Raw              int     `json:"raw"`
	NeverAccessed    int     `json:"never_accessed"`
	NeverAccessedPct float64 `json:"never_accessed_pct"`
}

// InsightsText summarizes the length of the stored text, in characters.
type InsightsText struct {
	AvgChars    float64 `json:"avg_chars"`
	MedianChars int     `json:"median_chars"`
	P90Chars    int     `json:"p90_chars"`
	MaxChars    int     `json:"max_chars"`
}

// ForgettingStats is what the next forget-sweep would do, computed with the
// same retention score (D22) and without changing anything.
type ForgettingStats struct {
	Eligible           int         `json:"eligible"`            // active, extracted, not pinned: the ones that decay
	Exempt             int         `json:"exempt"`              // active but never decays (raw or pinned)
	ColdNow            int         `json:"cold_now"`            // would be soft-deleted by a sweep right now
	SoftDeleted        int         `json:"soft_deleted"`        // waiting for the hard-delete
	HardDeleteDue      int         `json:"hard_delete_due"`     // soft-deleted past the grace window
	Threshold          float64     `json:"threshold"`           // retention floor
	AgeHalfLifeDays    float64     `json:"age_half_life_days"`  // D22 constants, for display
	GraceDays          int         `json:"grace_days"`          // reversible window
	RetentionHistogram []CountItem `json:"retention_histogram"` // eligible entries by retention score
}

// FilterRuleStats is one ingestion-filter rule with its in-memory counters
// (they reset when the server restarts).
type FilterRuleStats struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"` // "segment" or "fact"
	Description string `json:"description,omitempty"`
	Action      string `json:"action"` // effective action: drop, tag or off
	Seen        int64  `json:"seen"`
	Dropped     int64  `json:"dropped"`
	Tagged      int64  `json:"tagged"`
}

// FilterOverview reports the ingestion boilerplate filter (#53).
type FilterOverview struct {
	Mode         string            `json:"mode"` // global mode: default, off, tag or drop
	RulesVersion int               `json:"rules_version"`
	Rules        []FilterRuleStats `json:"rules"`
}

// InsightsOverviewResponse is the body of GET /insights/overview.
type InsightsOverviewResponse struct {
	GeneratedAt      time.Time       `json:"generated_at"`
	Totals           InsightsTotals  `json:"totals"`
	Text             InsightsText    `json:"text"`
	SoftDeletedByDay []CountItem     `json:"soft_deleted_by_day"`
	ByCategory       []CountItem     `json:"by_category"`
	BySource         []CountItem     `json:"by_source"`
	ByAge            []CountItem     `json:"by_age"`
	DomainCount      int             `json:"domain_count"`
	TopDomains       []DomainStat    `json:"top_domains"`
	VectorsIndexed   int             `json:"vectors_indexed"`
	Forgetting       ForgettingStats `json:"forgetting"`
	Filter           FilterOverview  `json:"filter"`
}

// ExampleEntry is a truncated, secret-free view of one memory.
type ExampleEntry struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Source string `json:"source"`
	Text   string `json:"text,omitempty"`
}

// PairExample is one near-duplicate pair.
type PairExample struct {
	Similarity float64      `json:"similarity"`
	A          ExampleEntry `json:"a"`
	B          ExampleEntry `json:"b"`
}

// ExactDupGroupExample is one group of identical (normalized) text.
type ExactDupGroupExample struct {
	Size    int            `json:"size"`
	Domains int            `json:"domains"`
	Text    string         `json:"text"`
	Members []ExampleEntry `json:"members"`
}

// ExactDupReport covers memories whose normalized text is identical.
type ExactDupReport struct {
	Groups            int                    `json:"groups"`
	Entries           int                    `json:"entries"`
	Redundant         int                    `json:"redundant"` // entries - groups
	SameDomainGroups  int                    `json:"same_domain_groups"`
	CrossDomainGroups int                    `json:"cross_domain_groups"`
	CrossSourceGroups int                    `json:"cross_source_groups"`
	Examples          []ExactDupGroupExample `json:"examples"`
}

// NearDupThreshold aggregates the pairs at or above one cosine threshold.
type NearDupThreshold struct {
	Threshold      float64 `json:"threshold"`
	Pairs          int     `json:"pairs"`
	Clusters       int     `json:"clusters"`
	Redundant      int     `json:"redundant"` // sum of (cluster size - 1)
	LargestCluster int     `json:"largest_cluster"`
	SameDomain     int     `json:"same_domain"`
	CrossDomain    int     `json:"cross_domain"`
	CrossSource    int     `json:"cross_source"`   // different source (tool)
	SameCapture    int     `json:"same_capture"`   // same source_content_ref
	NumbersDiffer  int     `json:"numbers_differ"` // likely distinct facts of the same shape
}

// NearDupBand is a group of pair examples (e.g. ">= 0.98").
type NearDupBand struct {
	Label    string        `json:"label"`
	Examples []PairExample `json:"examples"`
}

// NearDupReport covers semantically near-identical memories.
type NearDupReport struct {
	Available        bool               `json:"available"`
	Reason           string             `json:"reason,omitempty"` // why it is unavailable
	Method           string             `json:"method"`           // "exact" or "lsh"
	Indexed          int                `json:"indexed"`          // active vectors compared
	Tables           int                `json:"tables,omitempty"`
	Bits             int                `json:"bits,omitempty"`
	OversizedBuckets int                `json:"oversized_buckets,omitempty"`
	PairsTruncated   bool               `json:"pairs_truncated,omitempty"`
	Thresholds       []NearDupThreshold `json:"thresholds"`
	CrossSource      []CountItem        `json:"cross_source"` // source pairs at the lowest threshold
	Bands            []NearDupBand      `json:"bands"`
}

// NoiseCriterion is one low-value pattern and what it matched.
type NoiseCriterion struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Tier          string         `json:"tier"` // "conservative" or "review"
	Description   string         `json:"description"`
	Matches       int            `json:"matches"`
	NeverAccessed int            `json:"never_accessed"`
	Examples      []ExampleEntry `json:"examples"`
}

// NoiseReport lists low-value candidates among the "codigo" memories. It only
// reports: nothing is flagged, deleted or changed.
type NoiseReport struct {
	Category             string           `json:"category"`
	Scanned              int              `json:"scanned"`
	Criteria             []NoiseCriterion `json:"criteria"`
	ConservativeUnion    int              `json:"conservative_union"`    // distinct entries matching A, B or C
	ConservativeEligible int              `json:"conservative_eligible"` // ... never accessed, not pinned, extracted
	ReviewUnion          int              `json:"review_union"`          // distinct entries matching D, E or F
}

// HygieneReport is the result of one run of the hygiene analysis.
type HygieneReport struct {
	Version       int            `json:"version"`
	ComputedAt    time.Time      `json:"computed_at"`
	DurationMS    int64          `json:"duration_ms"`
	Partial       bool           `json:"partial"`
	PartialReason string         `json:"partial_reason,omitempty"`
	Scanned       int            `json:"scanned"` // active memories examined
	Exact         ExactDupReport `json:"exact_duplicates"`
	Near          NearDupReport  `json:"near_duplicates"`
	Noise         NoiseReport    `json:"noise"`
}

// HygieneStatus is the live state of the analysis job.
type HygieneStatus struct {
	Running    bool       `json:"running"`
	Phase      string     `json:"phase,omitempty"`
	Done       int        `json:"done"`
	Total      int        `json:"total"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

// HygieneResponse is the body of GET /insights/hygiene. Report is null until
// the analysis has run once.
type HygieneResponse struct {
	Status HygieneStatus  `json:"status"`
	Report *HygieneReport `json:"report"`
}

// HygieneStartResponse is the body of POST /insights/hygiene/run.
type HygieneStartResponse struct {
	Started bool `json:"started"`
}

// HygieneCancelResponse is the body of POST /insights/hygiene/cancel.
type HygieneCancelResponse struct {
	Cancelled bool `json:"cancelled"`
}
