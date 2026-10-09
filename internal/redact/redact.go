// Package redact removes secrets from content before it becomes an
// embedding, as decided in Phase 1 (inspired by getengram.app, see D6 in
// docs/decisions/). It always runs before any other step of the add
// pipeline (dedup, insert) — no secret should ever remain in indexed text.
package redact

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
)

const placeholder = "[REDACTED]"

// pattern is a named redaction rule, applied in order: specific patterns
// first, the generic high-entropy heuristic last.
type pattern struct {
	name string
	re   *regexp.Regexp
	// group is the capture group holding only the secret value. 0 replaces
	// the whole match; otherwise just the group is replaced, so the key name,
	// the auth scheme or the host survive and the fact stays useful.
	group int
	// skip, when set, vetoes a match whose value is not a real secret (a
	// placeholder, a variable reference, a description such as "required").
	skip func(value string) bool
}

// secretWords are the key words that mark a value as sensitive. They are
// matched anywhere inside a name (TEST_USER_PASSWORD, refresh_token,
// clientSecret), because `\b` does not see a boundary after "_".
const secretWords = `(?:password|passwd|passphrase|pwd|senha|secret[_-]?key|secret|segredo|token|api[_-]?key|access[_-]?key|private[_-]?key|chave)`

// kvValue is a quoted string (spaces allowed) or a bare run of characters.
const kvValue = `"[^"\n]*"|'[^'\n]*'|[^\s"'&,;]+`

// specificPatterns covers known secret formats. Order matters: PEM
// (multiline) before everything, specific key prefixes before generic
// key=value pairs.
var specificPatterns = []pattern{
	{name: "pem-private-key", re: regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)},
	{name: "openai-key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9]{16,}\b`)},
	{name: "openai-project-key", re: regexp.MustCompile(`\bsk-proj-[A-Za-z0-9_-]{20,}`)},
	{name: "anthropic-key", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}\b`)},
	{name: "aws-access-key-id", re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{name: "github-token", re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	{name: "github-fine-grained-token", re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`)},
	{name: "stripe-live-key", re: regexp.MustCompile(`\bsk_live_[A-Za-z0-9]{16,}\b`)},
	{name: "slack-token", re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{name: "google-api-key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{name: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
	{name: "connection-string", re: regexp.MustCompile(`(?i)\b(postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://[^\s"']+`)},
	// scheme://user:password@host — only the password is replaced.
	{name: "url-userinfo", re: regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]*://[^\s/:@"'<>]+:([^\s/@"'<>]+)@`), group: 1, skip: isPlaceholder},
	// Authorization: Basic|Bearer|Token ... — the scheme survives.
	{name: "authorization-header", re: regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*["']?(?:basic|bearer|token|digest)\s+([A-Za-z0-9._~+/=-]+)`), group: 1, skip: isPlaceholder},
	// KEY=value, key: value, "key": "value" — the key name survives.
	{name: "kv-secret", re: regexp.MustCompile(`(?i)` + secretWords + `["']?\s*(?::=|==|=|:)\s*(` + kvValue + `)`), group: 1, skip: isPlaceholder},
	// --password value, --token=value.
	{name: "cli-flag-secret", re: regexp.MustCompile(`(?i)(?:^|\s)--(?:password|passwd|pass|token|secret|api-key|apikey)(?:=|\s+)(` + kvValue + `)`), group: 1, skip: isPlaceholder},
	// Narrative forms: "a senha é 'X'", "the admin password for staging is X".
	{name: "narrative-secret", re: regexp.MustCompile(`(?i)\b(?:senha|password|passwd|passphrase|pwd|segredo|secret|token|api[_ -]?key|chave)\b(?:\s+[\p{L}\p{N}_-]+){0,6}?\s+(?:é|eh|era|is|was|=)\s+("[^"\n]{3,}"|'[^'\n]{3,}'|“[^”\n]{3,}”|\x60[^\x60\n]{3,}\x60|[^\s"'“”\x60,;]+)`), group: 1, skip: skipNarrative},
}

// Result describes what was redacted, for logging/observability (never
// contains the original value).
type Result struct {
	Text     string
	Redacted []string // names of the patterns that matched, in the order they matched
}

// Redact applies the specific patterns and then the high-entropy heuristic
// over the remaining tokens, returning text that is safe for
// embedding/indexing.
func Redact(text string) Result {
	redacted := []string{}

	for _, p := range specificPatterns {
		var hit bool
		text, hit = p.apply(text)
		if hit {
			redacted = append(redacted, p.name)
		}
	}

	text, hitEntropy := redactHighEntropyTokens(text)
	if hitEntropy {
		redacted = append(redacted, "high-entropy-token")
	}

	return Result{Text: text, Redacted: redacted}
}

// Scan returns the ids of the rules that would redact text, without ever
// returning any part of the value. Used by the maintenance scan.
func Scan(text string) []string { return Redact(text).Redacted }

func (p pattern) apply(text string) (string, bool) {
	if p.group == 0 {
		if !p.re.MatchString(text) {
			return text, false
		}
		return p.re.ReplaceAllString(text, placeholder), true
	}
	locs := p.re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return text, false
	}
	var b strings.Builder
	last := 0
	hit := false
	for _, loc := range locs {
		vs, ve := loc[2*p.group], loc[2*p.group+1]
		if vs < 0 || (p.skip != nil && p.skip(text[vs:ve])) {
			continue
		}
		b.WriteString(text[last:vs])
		b.WriteString(placeholder)
		last = ve
		hit = true
	}
	if !hit {
		return text, false
	}
	b.WriteString(text[last:])
	return b.String(), true
}

var (
	callPattern    = regexp.MustCompile(`^[A-Za-z_][\w.]*\(`)
	envRefPattern  = regexp.MustCompile(`(?i)^(?:process\.env|os\.environ|os\.getenv|env\.|secrets\.|vars\.)`)
	shellRefPatten = regexp.MustCompile(`^\$\{?[A-Za-z_]`)
	envNamePattern = regexp.MustCompile(`^[A-Z][A-Z]*(?:_[A-Z]+)+$`)
	maskPattern    = regexp.MustCompile(`^[*xX•.…#_-]+$`)
)

var nonSecretWords = map[string]bool{
	"string": true, "str": true, "int": true, "bool": true, "boolean": true, "required": true,
	"optional": true, "none": true, "null": true, "nil": true, "true": true, "false": true,
	"empty": true, "unset": true, "undefined": true, "redacted": true, "secret": true, "password": true,
	"senha": true, "token": true, "invalid": true, "valid": true, "correct": true, "incorrect": true,
	"wrong": true, "missing": true, "set": true, "ok": true, "bearer": true, "basic": true,
	"example": true, "placeholder": true, "obrigatorio": true, "obrigatório": true, "vazio": true, "pass": true,
}

// isPlaceholder reports whether value is obviously not a real secret: empty,
// masked, a variable or template reference, a function call, a type name or
// a description word.
func isPlaceholder(value string) bool {
	v := strings.Trim(value, "\"'`“”‘’")
	if v == "" || strings.HasPrefix(v, "[REDACTED") {
		return true
	}
	hasAlnum := false
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlnum = true
			break
		}
	}
	if !hasAlnum || maskPattern.MatchString(v) {
		return true
	}
	switch v[0] {
	case '<', '%':
		return true
	case '{':
		return true // {{ template }} or {placeholder}
	}
	lower := strings.ToLower(v)
	if shellRefPatten.MatchString(v) || envRefPattern.MatchString(v) || callPattern.MatchString(v) ||
		envNamePattern.MatchString(v) || nonSecretWords[lower] ||
		strings.HasPrefix(lower, "your") || strings.HasPrefix(lower, "<") {
		return true
	}
	if strings.HasPrefix(v, "=") { // "token := f()", "a == b"
		return true
	}
	return false
}

// skipNarrative is stricter than isPlaceholder for the free-text form: a
// quoted value is assumed to be the secret, but a bare word only counts when
// it looks like a password (digit, symbol or inner capital), so prose such as
// "the password is required" or "o reset de senha é feito" is left alone.
func skipNarrative(value string) bool {
	if isPlaceholder(value) {
		return true
	}
	if len(value) > 0 && strings.ContainsRune("\"'`“”", rune(value[0])) || strings.HasPrefix(value, "“") {
		return false
	}
	v := strings.TrimRight(value, ".,;:)")
	if len(v) < 4 {
		return true
	}
	for i, r := range v {
		switch {
		case unicode.IsDigit(r), strings.ContainsRune("!@#$%^&*+=?_/\\", r):
			return false
		case unicode.IsUpper(r) && i > 0:
			return false
		}
	}
	return true
}

// tokenPattern isolates generic token candidates: alphanumeric sequences
// (+ inner "_-.") of at least 20 characters, with no whitespace.
var tokenPattern = regexp.MustCompile(`\b[A-Za-z0-9_.\-]{20,}\b`)

const (
	minEntropyBitsPerChar = 3.2
	minTokenLength        = 20
)

// redactHighEntropyTokens is the last, generic step: any long token that
// "looks like" a random secret (high Shannon entropy, mix of character
// classes) but did not match any specific pattern above.
func redactHighEntropyTokens(text string) (string, bool) {
	hit := false
	out := tokenPattern.ReplaceAllStringFunc(text, func(tok string) string {
		if looksLikeSecret(tok) {
			hit = true
			return placeholder
		}
		return tok
	})
	return out, hit
}

func looksLikeSecret(tok string) bool {
	if len(tok) < minTokenLength {
		return false
	}
	if !hasMixedCharClasses(tok) {
		return false
	}
	if looksLikeIdentifier(tok) {
		return false
	}
	entropy := shannonEntropyPerChar(tok)
	if entropy < minEntropyBitsPerChar {
		return false
	}
	// Short-ish tokens (20-31 chars) are far more often names than keys:
	// demand all three classes or clearly random-looking entropy.
	if len(tok) < longTokenLength && charClasses(tok) < 3 && entropy < shortTokenMinEntropy {
		return false
	}
	return true
}

const (
	longTokenLength      = 32
	shortTokenMinEntropy = 4.0
)

// looksLikeIdentifier recognises names that the entropy heuristic used to
// destroy: camelCase / PascalCase functions, snake_case and kebab-case names,
// branch names, file names. Every segment (split on "_", "-", ".") longer than
// 4 characters must be a plain word (lower, UPPER, Capitalized or camelCase
// with a low share of capitals) or a number; a random key has mixed
// letter+digit segments, which makes the token fail this test.
func looksLikeIdentifier(tok string) bool {
	segs := strings.FieldsFunc(tok, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	words := 0
	for _, seg := range segs {
		if len(seg) <= 4 {
			continue
		}
		switch {
		case isDigits(seg):
			if len(seg) > 14 {
				return false
			}
		case isWordLike(seg):
			words++
		default:
			return false
		}
	}
	return words > 0
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// isWordLike reports whether s is letters only and does not look like random
// mixed-case noise (capitals are at most a third of the letters).
func isWordLike(s string) bool {
	upper := 0
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
		default:
			return false
		}
	}
	return upper*3 <= len(s)
}

func charClasses(tok string) int {
	var lower, upper, digit bool
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	n := 0
	for _, p := range []bool{lower, upper, digit} {
		if p {
			n++
		}
	}
	return n
}

// hasMixedCharClasses avoids false positives on ordinary text (long
// phrases, URLs without secrets, predictable identifiers): it requires at
// least 2 of the 3 classes (lowercase, uppercase, digit) to be present.
func hasMixedCharClasses(tok string) bool {
	var lower, upper, digit bool
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	classes := 0
	for _, present := range []bool{lower, upper, digit} {
		if present {
			classes++
		}
	}
	return classes >= 2
}

func shannonEntropyPerChar(s string) float64 {
	if s == "" {
		return 0
	}
	freq := make(map[rune]int, len(s))
	for _, r := range s {
		freq[r]++
	}
	n := float64(len(s))
	var entropy float64
	for _, count := range freq {
		p := float64(count) / n
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// String satisfies fmt.Stringer — useful for logging how many patterns
// matched without ever logging the content itself.
func (r Result) String() string {
	if len(r.Redacted) == 0 {
		return "no secrets detected"
	}
	return fmt.Sprintf("%d pattern(s) redacted: %s", len(r.Redacted), strings.Join(r.Redacted, ", "))
}
