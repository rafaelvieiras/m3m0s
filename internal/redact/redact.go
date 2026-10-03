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
)

const placeholder = "[REDACTED]"

// pattern is a named redaction rule, applied in order: specific patterns
// first, the generic high-entropy heuristic last.
type pattern struct {
	name string
	re   *regexp.Regexp
}

// specificPatterns covers known secret formats. Order matters: PEM
// (multiline) before everything, specific key prefixes before generic
// key=value pairs.
var specificPatterns = []pattern{
	{"pem-private-key", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)},
	{"openai-key", regexp.MustCompile(`\bsk-[A-Za-z0-9]{16,}\b`)},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}\b`)},
	{"aws-access-key-id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	{"stripe-live-key", regexp.MustCompile(`\bsk_live_[A-Za-z0-9]{16,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)},
	{"connection-string", regexp.MustCompile(`(?i)\b(postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://[^\s"']+`)},
	{"kv-secret", regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key|access[_-]?key)\b\s*[:=]\s*["']?[^\s"'&,;]+`)},
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
		if p.re.MatchString(text) {
			redacted = append(redacted, p.name)
			text = p.re.ReplaceAllString(text, placeholder)
		}
	}

	text, hitEntropy := redactHighEntropyTokens(text)
	if hitEntropy {
		redacted = append(redacted, "high-entropy-token")
	}

	return Result{Text: text, Redacted: redacted}
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
	if shannonEntropyPerChar(tok) < minEntropyBitsPerChar {
		return false
	}
	return true
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
