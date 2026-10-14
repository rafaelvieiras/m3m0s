package store

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/redact"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Text-level part of the hygiene analysis (D37): groups of identical text and
// low-value ("noise") patterns. It reads entries in small chunks from the
// in-memory index, never holds the store lock while matching, and never
// writes anything.

const (
	exampleMaxChars   = 160
	examplesPerRule   = 6
	exactGroupMembers = 4
	scanChunk         = 512

	noiseCategory = "code"
)

// pacer keeps a background analysis polite: it checks for cancellation and, when
// the duty cycle is below 100%, sleeps after each slice of work so the job does
// not monopolize a CPU the server also needs.
type pacer struct {
	pct  int
	last time.Time
}

func newPacer(pct int) *pacer {
	if pct <= 0 || pct > 100 {
		pct = 100
	}
	return &pacer{pct: pct, last: time.Now()}
}

// tick is called after each slice of work.
func (p *pacer) tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.pct < 100 {
		work := time.Since(p.last)
		if sleep := work * time.Duration(100-p.pct) / time.Duration(p.pct); sleep > 0 {
			t := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
	}
	p.last = time.Now()
	return nil
}

// foldRune lowercases r and strips the common Latin accents.
func foldRune(r rune) rune {
	r = unicode.ToLower(r)
	switch r {
	case 'á', 'à', 'â', 'ã', 'ä':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'õ', 'ö':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ç':
		return 'c'
	case 'ñ':
		return 'n'
	}
	return r
}

// normalizeText lowercases, folds Latin accents, drops punctuation and
// collapses whitespace: two memories with the same normalized text say the
// same thing.
func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r = foldRune(r); unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		} else {
			space = true
		}
	}
	return b.String()
}

// normalizedHash is the FNV-1a hash of normalizeText(s), computed without
// building the string (this runs once per memory).
func normalizedHash(s string) uint64 {
	const offset, prime = 14695981039346656037, 1099511628211
	h := uint64(offset)
	mix := func(b byte) { h = (h ^ uint64(b)) * prime }
	space := false
	started := false
	var buf [utf8.UTFMax]byte
	for _, r := range s {
		if r = foldRune(r); unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && started {
				mix(' ')
			}
			space, started = false, true
			n := utf8.EncodeRune(buf[:], r)
			for _, b := range buf[:n] {
				mix(b)
			}
		} else {
			space = true
		}
	}
	return h
}

var (
	phoneRe = regexp.MustCompile(`\+?\d{2}[\s.-]?\(?\d{2}\)?[\s.-]?\d{4,5}[\s.-]?\d{4}`)
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// hasSecret reports whether text matches any secret pattern of the ingestion
// redactor. Such entries are never shown as examples.
func hasSecret(text string) bool { return len(redact.Redact(text).Redacted) > 0 }

// exampleText truncates to ~160 characters and masks e-mail addresses and
// phone numbers.
func exampleText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	text = emailRe.ReplaceAllString(text, "[email]")
	text = phoneRe.ReplaceAllString(text, "[phone]")
	if utf8.RuneCountInString(text) > exampleMaxChars {
		r := []rune(text)
		text = string(r[:exampleMaxChars]) + "…"
	}
	return text
}

// exampleOf builds a safe example of e; ok is false when e must not be shown.
func exampleOf(e schema.Entry, withText bool) (apitypes.ExampleEntry, bool) {
	if hasSecret(e.Content) {
		return apitypes.ExampleEntry{}, false
	}
	ex := apitypes.ExampleEntry{ID: e.ID, Domain: e.Domain, Source: e.Source}
	if withText {
		ex.Text = exampleText(e.Content)
	}
	return ex, true
}

// ── noise rules ──

type noiseRule struct {
	id, name, tier, desc string
	match                func(e schema.Entry, lower string) bool
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func compileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

var (
	extractorLeakRe = compileAll(
		`objeto json no formato`, `\{"facts"`, `self-contained e reutiliz`, `n[ãa]o invent\w* informa`,
		`subdivis[õo]es artificiais`, `fato at[ôo]mico`,
	)
	compactRe = compileAll(
		`continua[çc][ãa]o de uma conversa anterior`, `continuation of a previous conversation`,
		`ran out of context`, `resumo (abaixo )?cobre`, `summary below covers`,
		`(comando|command)\W{0,3}/compact`, `compacta[çc][ãa]o\W{0,3}/compact`,
	)
	commitGenRe = compileAll(
		`mensagem de commit.{0,160}(git diff|staged|stdin)`, `commit message.{0,160}(git diff|staged|stdin)`,
		`commit.{0,80}git diff`,
	)
	diffTriviaRe = compileAll(
		`\.(png|jpe?g|gif|webp|svg)\b.{0,60}\b\d[\d.,]*\s*bytes`,
		`\b\d+\s+(inser[çc][õo]es|dele[çc][õo]es|insertions?|deletions?)\b`,
		`passou de 0 para`,
	)
	metaDescRe = compileAll(
		`^(o|este|esse|the)\s+texto\s+(descreve|cont[ée]m|apresenta|lista|[ée] um|fornecido)`,
		`^the text (describes|contains|presents|lists|is a)\b`,
	)
)

// shortNoIdentifier: under 40 characters and nothing that looks like an
// identifier (digits, slashes, backticks, underscores, inner dots).
func shortNoIdentifier(e schema.Entry) bool {
	t := strings.TrimSpace(e.Content)
	if utf8.RuneCountInString(t) >= 40 || !neverAccessed(e) {
		return false
	}
	t = strings.TrimRight(t, ".!? ")
	return !strings.ContainsAny(t, "/`_.") && strings.IndexFunc(t, unicode.IsDigit) < 0
}

var noiseRules = []noiseRule{
	{"A", "Extractor prompt leak", "conservative",
		"Facts the extractor derived from its own instructions (JSON output format, \"atomic and self-contained\", \"do not invent\"). Extracted entries only; synced docs excluded.",
		func(e schema.Entry, l string) bool {
			return hasSourceRef(e) && e.Source != "selfdocs" && anyMatch(extractorLeakRe, l)
		}},
	{"B", "Compact / continuation boilerplate", "conservative",
		"Text produced by /compact or by a session continuation (\"the summary below covers the earlier portion\", \"ran out of context\").",
		func(e schema.Entry, l string) bool { return anyMatch(compactRe, l) }},
	{"C", "Commit-message generator", "conservative",
		"The fixed prompt of a commit-message generator (\"write a commit message from a staged git diff\").",
		func(e schema.Entry, l string) bool { return anyMatch(commitGenRe, l) }},
	{"D", "Diff trivia", "review",
		"File sizes in bytes, insertion/deletion counts, \"went from 0 to N bytes\". May be the only record of a commit.",
		func(e schema.Entry, l string) bool { return anyMatch(diffTriviaRe, l) }},
	{"E", "Meta-description", "review",
		"Starts by describing the text (\"The text describes...\") instead of stating the fact. Some carry useful content.",
		func(e schema.Entry, l string) bool { return anyMatch(metaDescRe, strings.TrimSpace(l)) }},
	{"F", "Short, no identifier", "review",
		"Under 40 characters, never accessed, with no number, path, backtick, underscore or inner dot. Often context-free.",
		func(e schema.Entry, _ string) bool { return shortNoIdentifier(e) }},
}

// ── the scan ──

type dupGroup struct {
	count       int
	members     []string
	text        string
	unsafeText  bool
	firstDomain string
	firstSource string
	mixedDomain bool
	mixedSource bool
	domains     map[string]struct{}
}

type textScan struct {
	active int
	exact  apitypes.ExactDupReport
	noise  apitypes.NoiseReport
}

// scanText walks every active entry once, in a deterministic order, and fills
// the exact-duplicate and noise reports. progress is called after each chunk.
func (s *MemoryStore) scanText(ctx context.Context, pc *pacer, progress nearProgress) (textScan, error) {
	s.mu.RLock()
	ids := make([]string, 0, len(s.index))
	for id := range s.index {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)

	var res textScan
	res.noise = apitypes.NoiseReport{Category: noiseCategory, Criteria: make([]apitypes.NoiseCriterion, len(noiseRules))}
	for i, r := range noiseRules {
		res.noise.Criteria[i] = apitypes.NoiseCriterion{ID: r.id, Name: r.name, Tier: r.tier, Description: r.desc, Examples: []apitypes.ExampleEntry{}}
	}

	first := make(map[uint64]string, len(ids)) // hash of normalized text -> id of the first entry
	groups := map[uint64]*dupGroup{}
	chunk := make([]schema.Entry, 0, scanChunk)

	for start := 0; start < len(ids); start += scanChunk {
		end := min(start+scanChunk, len(ids))
		chunk = chunk[:0]
		s.mu.RLock()
		for _, id := range ids[start:end] {
			if e, ok := s.index[id]; ok && !isSoftDeleted(e) {
				chunk = append(chunk, e)
			}
		}
		s.mu.RUnlock()

		for _, e := range chunk {
			res.active++
			h := normalizedHash(e.Content)
			if firstID, seen := first[h]; !seen {
				first[h] = e.ID
			} else {
				g := groups[h]
				if g == nil {
					g = &dupGroup{members: []string{firstID}, domains: map[string]struct{}{}}
					groups[h] = g
					g.count = 1
					if f, ok := s.entryByID(firstID); ok {
						g.text, g.unsafeText = exampleText(f.Content), hasSecret(f.Content)
						g.firstDomain, g.firstSource = f.Domain, f.Source
						g.domains[f.Domain] = struct{}{}
					}
				}
				g.count++
				if len(g.members) < exactGroupMembers {
					g.members = append(g.members, e.ID)
				}
				if e.Domain != g.firstDomain {
					g.mixedDomain = true
				}
				if e.Source != g.firstSource {
					g.mixedSource = true
				}
				g.domains[e.Domain] = struct{}{}
			}

			if e.DomainCategory == noiseCategory {
				scanNoise(&res.noise, e)
			}
		}
		if progress != nil {
			progress(end, len(ids), "scanning text")
		}
		if err := pc.tick(ctx); err != nil {
			return res, err
		}
	}

	s.finishExact(&res.exact, groups)
	return res, nil
}

func (s *MemoryStore) entryByID(id string) (schema.Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.index[id]
	return e, ok
}

// scanNoise applies every rule to e and updates the counters and examples.
func scanNoise(n *apitypes.NoiseReport, e schema.Entry) {
	n.Scanned++
	lower := strings.ToLower(e.Content)
	var conservative, review bool
	for i, r := range noiseRules {
		if !r.match(e, lower) {
			continue
		}
		c := &n.Criteria[i]
		c.Matches++
		if neverAccessed(e) {
			c.NeverAccessed++
		}
		if len(c.Examples) < examplesPerRule {
			if ex, ok := exampleOf(e, true); ok {
				c.Examples = append(c.Examples, ex)
			}
		}
		if r.tier == "conservative" {
			conservative = true
		} else {
			review = true
		}
	}
	if conservative {
		n.ConservativeUnion++
		if neverAccessed(e) && !isPinned(e) && hasSourceRef(e) {
			n.ConservativeEligible++
		}
	}
	if review {
		n.ReviewUnion++
	}
}

// finishExact turns the collected groups into the report.
func (s *MemoryStore) finishExact(out *apitypes.ExactDupReport, groups map[uint64]*dupGroup) {
	out.Examples = []apitypes.ExactDupGroupExample{}
	list := make([]*dupGroup, 0, len(groups))
	for _, g := range groups {
		out.Groups++
		out.Entries += g.count
		if g.mixedDomain {
			out.CrossDomainGroups++
		} else {
			out.SameDomainGroups++
		}
		if g.mixedSource {
			out.CrossSourceGroups++
		}
		list = append(list, g)
	}
	out.Redundant = out.Entries - out.Groups
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].members[0] < list[j].members[0]
	})
	for _, g := range list {
		if len(out.Examples) >= examplesPerRule {
			break
		}
		if g.unsafeText {
			continue
		}
		ex := apitypes.ExactDupGroupExample{Size: g.count, Domains: len(g.domains), Text: g.text}
		for _, id := range g.members {
			if e, ok := s.entryByID(id); ok {
				if m, ok := exampleOf(e, false); ok {
					ex.Members = append(ex.Members, m)
				}
			}
		}
		out.Examples = append(out.Examples, ex)
	}
}
