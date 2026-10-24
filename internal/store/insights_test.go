package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

const testDims = 32

// insightsStore returns a store whose vector index is empty and sized for
// testDims; entries are seeded through seedEntry so the database, the
// in-memory index and the vector index agree.
func insightsStore(t *testing.T) *MemoryStore {
	t.Helper()
	s := newTestStore(t)
	s.vecs.reset(testDims)
	return s
}

type seed struct {
	id, content, domain, source string
	md                          map[string]string
	vec                         []float32
	age                         time.Duration
}

func seedEntry(t *testing.T, s *MemoryStore, sd seed) schema.Entry {
	t.Helper()
	if sd.source == "" {
		sd.source = "claude-code"
	}
	if sd.domain == "" {
		sd.domain = "code:proj"
	}
	created := time.Now().Add(-sd.age)
	md := map[string]string{}
	for k, v := range sd.md {
		md[k] = v
	}
	e := schema.Entry{
		ID: sd.id, Content: sd.content, Domain: sd.domain, DomainCategory: schema.DomainCategory(sd.domain),
		Source: sd.source, Tags: []string{}, Metadata: md, CreatedAt: created, UpdatedAt: created,
	}
	if err := s.db.UpsertEntry(context.Background(), entryToRow(e, sd.vec)); err != nil {
		t.Fatalf("seed %s: %v", sd.id, err)
	}
	setIndexEntry(s, e)
	if sd.vec != nil {
		s.vecs.upsert(e.ID, e.Domain, sd.vec)
	}
	return e
}

func extractedMD(extra ...string) map[string]string {
	md := map[string]string{metadataKeySourceContentRef: "ref-1"}
	for i := 0; i+1 < len(extra); i += 2 {
		md[extra[i]] = extra[i+1]
	}
	return md
}

func randUnit(r *rand.Rand) []float32 {
	v := make([]float32, testDims)
	var n float64
	for i := range v {
		v[i] = float32(r.NormFloat64())
		n += float64(v[i]) * float64(v[i])
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / math.Sqrt(n))
	}
	return v
}

// withCosine returns a unit vector whose cosine to base is exactly cos.
func withCosine(r *rand.Rand, base []float32, cos float64) []float32 {
	o := randUnit(r)
	var dot float64
	for i := range o {
		dot += float64(o[i]) * float64(base[i])
	}
	var n float64
	for i := range o {
		o[i] -= float32(dot) * base[i]
		n += float64(o[i]) * float64(o[i])
	}
	sin := math.Sqrt(1 - cos*cos)
	out := make([]float32, testDims)
	for i := range out {
		out[i] = float32(cos)*base[i] + float32(sin*float64(o[i])/math.Sqrt(n))
	}
	return out
}

func TestInsightsOverviewCountsAndForgettingMatchesSweep(t *testing.T) {
	s := insightsStore(t)
	now := time.Now()
	day := 24 * time.Hour
	ref := extractedMD()

	seedEntry(t, s, seed{id: "a1", content: "fresh extracted fact", md: ref, age: day})
	seedEntry(t, s, seed{id: "a2", content: "old extracted fact never read", md: ref, age: 200 * day, domain: "code:other"})
	seedEntry(t, s, seed{id: "a3", content: "old but pinned", md: extractedMD(metadataKeyPinned, "true"), age: 200 * day})
	seedEntry(t, s, seed{id: "a4", content: "raw content", age: 200 * day, source: "selfdocs"})
	seedEntry(t, s, seed{id: "a5", content: "expired", md: extractedMD(metadataKeyValidUntil, now.Add(-day).UTC().Format(time.RFC3339)), age: 3 * day})
	seedEntry(t, s, seed{id: "a6", content: "future validity", md: extractedMD(metadataKeyValidUntil, now.Add(day).UTC().Format(time.RFC3339), metadataKeyAccessCount, "3"), age: 3 * day})
	seedEntry(t, s, seed{id: "d1", content: "deleted recently", md: extractedMD(metadataKeyDeletedAt, now.Add(-2*day).UTC().Format(time.RFC3339)), age: 50 * day})
	seedEntry(t, s, seed{id: "d2", content: "deleted long ago", md: extractedMD(metadataKeyDeletedAt, now.Add(-45*day).UTC().Format(time.RFC3339)), age: 90 * day})

	ov := s.InsightsOverview(now)
	tot := ov.Totals
	want := apitypes.InsightsTotals{Total: 8, Active: 6, SoftDeleted: 2, WithValidUntil: 2, Expired: 1, Pinned: 1, Extracted: 5, Raw: 1, NeverAccessed: 5}
	want.NeverAccessedPct = tot.NeverAccessedPct
	if tot != want {
		t.Errorf("totals = %+v, want %+v", tot, want)
	}
	if math.Abs(tot.NeverAccessedPct-5.0/6.0*100) > 1e-9 {
		t.Errorf("never accessed pct = %v", tot.NeverAccessedPct)
	}
	if ov.DomainCount != 2 || ov.TopDomains[0].Domain != "code:proj" || ov.TopDomains[0].Count != 5 {
		t.Errorf("domains = %+v", ov.TopDomains)
	}
	if len(ov.SoftDeletedByDay) != 2 {
		t.Errorf("soft-deleted by day = %+v", ov.SoftDeletedByDay)
	}
	if ov.VectorsIndexed != 0 {
		t.Errorf("vectors indexed = %d, want 0", ov.VectorsIndexed)
	}
	var ages int
	for _, b := range ov.ByAge {
		ages += b.Count
	}
	if ages != 6 {
		t.Errorf("age buckets add up to %d, want 6", ages)
	}

	// The forgetting outlook must agree with what a dry-run sweep reports.
	sweep, err := s.ForgetSweep(context.Background(), true, now)
	if err != nil {
		t.Fatal(err)
	}
	f := ov.Forgetting
	if f.ColdNow != len(sweep.SoftDeleteCandidates) || f.HardDeleteDue != len(sweep.HardDeleteCandidates) || f.Eligible != sweep.Evaluated {
		t.Errorf("forgetting %+v disagrees with dry-run sweep %+v", f, sweep)
	}
	if f.ColdNow != 1 || f.HardDeleteDue != 1 || f.SoftDeleted != 2 || f.Exempt != 1 {
		t.Errorf("forgetting = %+v", f)
	}
	var hist int
	for _, b := range f.RetentionHistogram {
		hist += b.Count
	}
	if hist != f.Eligible {
		t.Errorf("retention histogram adds up to %d, want %d", hist, f.Eligible)
	}
}

// The built-in docs (source "selfdocs") are not the user's memories: no part
// of Insights counts them.
func TestInsightsIgnoreBuiltinDocs(t *testing.T) {
	s := insightsStore(t)
	text := "The deploy pipeline signs the release checksums after the tests pass."
	seedEntry(t, s, seed{id: "u1", content: text, md: extractedMD()})
	for i := 0; i < 5; i++ {
		// same text as the user memory, so a leak would show up as duplicates
		seedEntry(t, s, seed{id: fmt.Sprintf("doc%d", i), content: text, source: "selfdocs"})
	}

	ov := s.InsightsOverviewExcluding(time.Now(), "selfdocs")
	if ov.Totals.Total != 1 || ov.Totals.Active != 1 {
		t.Errorf("totals = %+v, want 1 user memory", ov.Totals)
	}
	kinds := 0
	for _, k := range ov.ByKind {
		kinds += k.Count
	}
	if kinds != 1 {
		t.Errorf("by kind adds up to %d, want 1", kinds)
	}
	if f := ov.Forgetting; f.Exempt != 0 || f.Eligible != 1 {
		t.Errorf("forgetting = %+v, built-in docs must not be counted", f)
	}

	rep, err := runAnalysis(t, s, InsightsOptions{CPUPercent: 100})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 1 || rep.Exact.Groups != 0 {
		t.Errorf("hygiene scanned %d, exact groups %d; want 1 and 0", rep.Scanned, rep.Exact.Groups)
	}
}

func TestHygieneExactDuplicatesAndNoise(t *testing.T) {
	s := insightsStore(t)
	ref := extractedMD()
	day := 24 * time.Hour

	// Identical after normalization: case, accents, punctuation, spacing.
	seedEntry(t, s, seed{id: "x1", content: "The résumé of the café is defined in the project configuration.", md: ref, domain: "code:one"})
	seedEntry(t, s, seed{id: "x2", content: "the resume of the cafe is defined in the project configuration", md: ref, domain: "code:two", source: "pi"})
	seedEntry(t, s, seed{id: "x3", content: "A  lone fact that is long enough to avoid the short rule.", md: ref})
	seedEntry(t, s, seed{id: "y1", content: "Same domain duplicate of a sufficiently long fact text!", md: ref, domain: "code:one"})
	seedEntry(t, s, seed{id: "y2", content: "same domain duplicate of a sufficiently long fact text", md: ref, domain: "code:one"})
	// Soft-deleted copies never count.
	seedEntry(t, s, seed{id: "z1", content: "same domain duplicate of a sufficiently long fact text", md: extractedMD(metadataKeyDeletedAt, time.Now().UTC().Format(time.RFC3339))})

	// Noise: one per rule, plus negatives.
	seedEntry(t, s, seed{id: "nA", content: `The response must be ONLY a JSON object in the format {"facts": ["fact 1"]}.`, md: ref, age: 20 * day})
	seedEntry(t, s, seed{id: "nA2", content: `If the text is already a single atomic fact, return it as is.`, md: ref, source: "selfdocs"}) // docs excluded
	seedEntry(t, s, seed{id: "nB", content: "The session is a continuation of a previous conversation that ran out of context.", md: ref})
	seedEntry(t, s, seed{id: "nC", content: "Generate a commit message in English from a staged git diff.", md: ref})
	seedEntry(t, s, seed{id: "nD", content: "The file dashboard.png was added with 170806 bytes.", md: ref})
	seedEntry(t, s, seed{id: "nE", content: "The text describes four families of functions.", md: ref})
	seedEntry(t, s, seed{id: "nF", content: "The task and the brief were created.", md: ref})
	seedEntry(t, s, seed{id: "okF", content: "STUMP_PORT defaults to 10801", md: ref})                                                    // has identifier
	seedEntry(t, s, seed{id: "okA", content: "The release pipeline signs the checksums file with cosign after the tests pass.", md: ref}) // legit
	seedEntry(t, s, seed{id: "nB2", content: "The summary below covers the earlier portion of the conversation.", md: extractedMD(metadataKeyAccessCount, "4")})
	// Other category: scanned for duplicates but not for noise.
	seedEntry(t, s, seed{id: "other", content: "The summary below covers the earlier portion of the conversation.", domain: "personal:diary", md: ref})
	// A secret-bearing noise match counts but is never shown; phone numbers are masked.
	seedEntry(t, s, seed{id: "sec", content: "O resumo cobre a parte anterior; token: sk-abcdefghijklmnopqrstuvwxyz0123456789", md: ref})
	seedEntry(t, s, seed{id: "tel", content: "The text describes the contact +55 41 99999-8888 and the e-mail someone@example.com of the client.", md: ref})

	rep, err := runAnalysis(t, s, InsightsOptions{CPUPercent: 100})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Partial {
		t.Fatalf("unexpected partial report: %s", rep.PartialReason)
	}

	ex := rep.Exact
	if ex.Groups != 3 || ex.Entries != 6 || ex.Redundant != 3 {
		t.Errorf("exact duplicates = %+v", ex)
	}
	if ex.CrossDomainGroups < 2 || ex.SameDomainGroups < 1 || ex.CrossSourceGroups < 1 {
		t.Errorf("exact breakdown = %+v", ex)
	}

	by := map[string]apitypes.NoiseCriterion{}
	for _, c := range rep.Noise.Criteria {
		by[c.ID] = c
	}
	checks := map[string]int{"A": 1, "B": 3, "C": 1, "D": 1, "E": 2, "F": 1}
	for id, want := range checks {
		if got := by[id].Matches; got != want {
			t.Errorf("criterion %s matches = %d, want %d (%+v)", id, got, want, by[id].Examples)
		}
	}
	if by["A"].Tier != "conservative" || by["D"].Tier != "review" {
		t.Errorf("tiers wrong: A=%s D=%s", by["A"].Tier, by["D"].Tier)
	}
	if by["B"].NeverAccessed != 2 {
		t.Errorf("B never accessed = %d, want 2 (nB2 was read)", by["B"].NeverAccessed)
	}
	if rep.Noise.ConservativeUnion != 5 || rep.Noise.ConservativeEligible != 4 || rep.Noise.ReviewUnion != 4 {
		t.Errorf("unions = %+v", rep.Noise)
	}
	if rep.Noise.Scanned != 16 {
		t.Errorf("noise scanned = %d, want 16 (code only, built-in docs excluded)", rep.Noise.Scanned)
	}

	raw, _ := json.Marshal(rep)
	js := string(raw)
	for _, banned := range []string{"sk-abcdefgh", "99999-8888", "someone@example.com", `"sec"`} {
		if strings.Contains(js, banned) {
			t.Errorf("report leaks %q", banned)
		}
	}
	if !strings.Contains(js, "[phone]") || !strings.Contains(js, "[email]") {
		t.Errorf("phone/e-mail were not masked in examples")
	}
}

func TestHygieneExampleTruncation(t *testing.T) {
	long := strings.Repeat("palavra ", 80)
	got := exampleText(long)
	if n := len([]rune(got)); n > exampleMaxChars+1 {
		t.Errorf("example has %d runes, want <= %d", n, exampleMaxChars+1)
	}
}

func TestHygieneNearDuplicatesExactMode(t *testing.T) {
	s := insightsStore(t)
	r := rand.New(rand.NewSource(1))
	ref := extractedMD()

	base := randUnit(r)
	seedEntry(t, s, seed{id: "b0", content: "base fact 17", md: ref, domain: "code:one", vec: base})
	seedEntry(t, s, seed{id: "b1", content: "base fact 17 again", md: ref, domain: "code:two", vec: withCosine(r, base, 0.99), source: "pi"})
	other := randUnit(r)
	seedEntry(t, s, seed{id: "c0", content: "other fact", md: ref, domain: "code:one", vec: other})
	seedEntry(t, s, seed{id: "c1", content: "other fact two", md: ref, domain: "code:one", vec: withCosine(r, other, 0.96)})
	third := randUnit(r)
	seedEntry(t, s, seed{id: "d0", content: "third fact 1", md: ref, domain: "code:one", vec: third})
	seedEntry(t, s, seed{id: "d1", content: "third fact 2", md: ref, domain: "code:one", vec: withCosine(r, third, 0.935)})
	// A soft-deleted twin and unrelated noise vectors must not matter.
	seedEntry(t, s, seed{id: "gone", content: "base fact 17", md: extractedMD(metadataKeyDeletedAt, time.Now().UTC().Format(time.RFC3339)), vec: withCosine(r, base, 0.999)})
	for i := 0; i < 20; i++ {
		seedEntry(t, s, seed{id: fmt.Sprintf("u%02d", i), content: fmt.Sprintf("unrelated %d", i), md: ref, vec: randUnit(r)})
	}

	rep, err := runAnalysis(t, s, InsightsOptions{CPUPercent: 100})
	if err != nil {
		t.Fatal(err)
	}
	n := rep.Near
	if !n.Available || n.Method != "exact" || n.Indexed != 26 {
		t.Fatalf("near = %+v", n)
	}
	th := n.Thresholds
	// >=0.98: b0-b1 ; >=0.95: + c0-c1 ; >=0.92: + d0-d1
	if th[0].Pairs != 1 || th[1].Pairs != 2 || th[2].Pairs != 3 {
		t.Errorf("pairs by threshold = %d/%d/%d, want 1/2/3 (%+v)", th[0].Pairs, th[1].Pairs, th[2].Pairs, th)
	}
	if th[0].CrossDomain != 1 || th[0].SameDomain != 0 || th[0].CrossSource != 1 {
		t.Errorf(">=0.98 breakdown = %+v", th[0])
	}
	if th[2].SameDomain != 2 || th[2].CrossDomain != 1 || th[2].Clusters != 3 || th[2].Redundant != 3 || th[2].LargestCluster != 2 {
		t.Errorf(">=0.92 breakdown = %+v", th[2])
	}
	if th[0].NumbersDiffer != 0 || th[2].NumbersDiffer != 1 {
		t.Errorf("numbers differ: %d / %d (only d0-d1 differ)", th[0].NumbersDiffer, th[2].NumbersDiffer)
	}
	if len(n.CrossSource) != 1 || n.CrossSource[0].Key != "claude-code x pi" {
		t.Errorf("cross source = %+v", n.CrossSource)
	}
	if len(n.Bands) != 4 || len(n.Bands[0].Examples) != 1 || len(n.Bands[1].Examples) != 1 || len(n.Bands[3].Examples) != 1 {
		t.Errorf("bands = %+v", n.Bands)
	}
	if len(n.Bands[2].Examples) != 0 {
		t.Errorf("no cross-domain 0.95-0.98 pair exists, got %+v", n.Bands[2])
	}
	if e := n.Bands[0].Examples[0]; e.Similarity < 0.985 || e.Similarity > 0.995 {
		t.Errorf("example similarity %v", e.Similarity)
	}
}

func TestHygieneNearDuplicatesLSHRecall(t *testing.T) {
	s := insightsStore(t)
	r := rand.New(rand.NewSource(3))
	ref := extractedMD()
	type planted struct{ a, b string }
	var pl []planted
	const clusters = 600
	for i := 0; i < clusters; i++ {
		v := randUnit(r)
		a := fmt.Sprintf("p%04da", i)
		seedEntry(t, s, seed{id: a, content: "fact " + a, md: ref, vec: v})
		if i%3 == 0 {
			b := fmt.Sprintf("p%04db", i)
			seedEntry(t, s, seed{id: b, content: "fact " + b, md: ref, vec: withCosine(r, v, 0.97)})
			pl = append(pl, planted{a, b})
		}
	}
	opts := InsightsOptions{CPUPercent: 100, near: nearParams{exactBelow: 10, tables: 24, bits: 8}}
	rep, err := runAnalysis(t, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Near.Method != "lsh" {
		t.Fatalf("method = %s, want lsh", rep.Near.Method)
	}
	// Pairs of random 32-d vectors can themselves exceed 0.92 by chance, so
	// require every planted pair (cosine 0.97) and a sane total.
	if got := rep.Near.Thresholds[1].Pairs; got < len(pl) || got > len(pl)+20 {
		t.Errorf(">=0.95 pairs = %d, want about %d", got, len(pl))
	}

	// Cross-check against brute force.
	exactRep, err := runAnalysis(t, s, InsightsOptions{CPUPercent: 100, near: nearParams{exactBelow: 100000}})
	if err != nil {
		t.Fatal(err)
	}
	ex, ls := exactRep.Near.Thresholds[2].Pairs, rep.Near.Thresholds[2].Pairs
	if ls > ex || float64(ls) < 0.9*float64(ex) {
		t.Errorf("lsh found %d pairs >= 0.92, brute force %d (want recall >= 90%%)", ls, ex)
	}
}

func TestHygieneReportRoundTripAndNothingInEntriesChanges(t *testing.T) {
	s := insightsStore(t)
	r := rand.New(rand.NewSource(5))
	for i := 0; i < 15; i++ {
		seedEntry(t, s, seed{id: fmt.Sprintf("e%02d", i), content: fmt.Sprintf("fact number %d", i%7), md: extractedMD(), vec: randUnit(r)})
	}
	ctx := context.Background()
	before := dumpEntries(t, s)
	idxBefore := indexFingerprint(s)

	if rep, err := s.HygieneReport(ctx); err != nil || rep != nil {
		t.Fatalf("before any run: report=%v err=%v", rep, err)
	}
	summary, err := s.RunHygieneAnalysis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary == "" {
		t.Error("empty summary")
	}
	rep, err := s.HygieneReport(ctx)
	if err != nil || rep == nil {
		t.Fatalf("report=%v err=%v", rep, err)
	}
	if rep.Scanned != 15 || rep.ComputedAt.IsZero() || rep.Version != hygieneReportVersion {
		t.Errorf("report = %+v", rep)
	}
	if st := s.HygieneStatus(); st.Running || st.FinishedAt == nil || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
	if after := dumpEntries(t, s); !reflect.DeepEqual(before, after) {
		t.Error("the analysis changed rows of the entries table")
	}
	if idxBefore != indexFingerprint(s) {
		t.Error("the analysis changed the in-memory index")
	}

	// A second run replaces the report.
	if _, err := s.RunHygieneAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHygieneCancelKeepsPreviousReport(t *testing.T) {
	s := insightsStore(t)
	r := rand.New(rand.NewSource(9))
	for i := 0; i < 200; i++ {
		seedEntry(t, s, seed{id: fmt.Sprintf("e%03d", i), content: fmt.Sprintf("fact %d", i), md: extractedMD(), vec: randUnit(r)})
	}
	if _, err := s.RunHygieneAnalysis(context.Background()); err != nil {
		t.Fatal(err)
	}
	prev, _ := s.HygieneReport(context.Background())

	// Heavily throttled run, cancelled while it sleeps between slices.
	s.SetInsightsOptions(InsightsOptions{CPUPercent: 1, MaxDuration: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.RunHygieneAnalysis(ctx); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for !s.HygieneStatus().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled run must return an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation was not honored")
	}
	if st := s.HygieneStatus(); st.Running || st.LastError == "" {
		t.Errorf("status after cancel = %+v", st)
	}
	cur, _ := s.HygieneReport(context.Background())
	if cur == nil || !cur.ComputedAt.Equal(prev.ComputedAt) {
		t.Error("a cancelled run must not replace the stored report")
	}
}

func TestHygieneTimeLimitGivesPartialReport(t *testing.T) {
	s := insightsStore(t)
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 100; i++ {
		seedEntry(t, s, seed{id: fmt.Sprintf("e%03d", i), content: fmt.Sprintf("fact %d", i), md: extractedMD(), vec: randUnit(r)})
	}
	s.SetInsightsOptions(InsightsOptions{CPUPercent: 1, MaxDuration: 5 * time.Millisecond})
	if _, err := s.RunHygieneAnalysis(context.Background()); err != nil {
		t.Fatal(err)
	}
	rep, _ := s.HygieneReport(context.Background())
	if rep == nil || !rep.Partial || rep.PartialReason == "" {
		t.Errorf("want a partial report, got %+v", rep)
	}
}

func TestHygieneKeywordOnlyHasNoNearSection(t *testing.T) {
	s := insightsStore(t)
	s.emb.mu.Lock()
	s.emb.emb = nil
	s.emb.mu.Unlock()
	s.vecs.reset(0)
	seedEntry(t, s, seed{id: "k1", content: "just text", md: extractedMD()})
	rep, err := runAnalysis(t, s, InsightsOptions{CPUPercent: 100})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Near.Available || rep.Near.Reason == "" || rep.Scanned != 1 {
		t.Errorf("report = %+v", rep)
	}
}

func TestNormalizedHashMatchesNormalizeText(t *testing.T) {
	for _, in := range []string{"  It is  FORBIDDEN, to invent!  ", "the resume of the cafe", "", "!!!", "Action 42 — café", "日本語 テスト"} {
		var h uint64 = 14695981039346656037
		for _, b := range []byte(normalizeText(in)) {
			h = (h ^ uint64(b)) * 1099511628211
		}
		if got := normalizedHash(in); got != h {
			t.Errorf("normalizedHash(%q) disagrees with normalizeText", in)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	a, b := normalizeText("  It is  FORBIDDEN, to invent! Café  "), normalizeText("it is forbidden to invent cafe")
	if a != b || a == "" {
		t.Errorf("%q != %q", a, b)
	}
}

// ── helpers ──

func runAnalysis(t *testing.T, s *MemoryStore, opts InsightsOptions) (apitypes.HygieneReport, error) {
	t.Helper()
	opts.MaxDuration = time.Minute
	rep, err := s.analyze(context.Background(), opts.withDefaults(), time.Now())
	return rep, err
}

func dumpEntries(t *testing.T, s *MemoryStore) string {
	t.Helper()
	rows, err := s.db.LoadAllEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rows)
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	embs, err := s.db.EmbeddingsByIDs(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := json.Marshal(embs)
	return string(raw) + string(raw2)
}

func indexFingerprint(s *MemoryStore) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, _ := json.Marshal(s.index)
	return string(raw)
}
