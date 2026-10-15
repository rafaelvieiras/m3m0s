// Package fuzzy corrects misspelled query terms against the vocabulary of the
// memory store (D51): edit distance bounded by term length, restricted to
// candidates that are clearly more common than the term itself, so a typo of
// a known name is repaired while a rare-but-correct word is left alone.
package fuzzy

import (
	"strings"
	"unicode"
)

// Fold lowercases s and strips the diacritics of Latin letters, so
// "Über" and "uber" compare equal.
func Fold(s string) string { return folder.Replace(strings.ToLower(s)) }

var folder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y", "ÿ", "y", "ß", "ss",
)

// Words splits text into folded words of letters and digits.
func Words(text string) []string {
	return strings.FieldsFunc(Fold(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Correctable is the minimum length of a term worth correcting: shorter
// terms are too ambiguous (one edit away from many unrelated words).
const Correctable = 5

// MaxEdits is the largest edit distance accepted between a term of n runes
// and its correction: none below Correctable (5), one up to 7 runes, two beyond.
func MaxEdits(n int) int {
	switch {
	case n < Correctable:
		return 0
	case n <= 7:
		return 1
	default:
		return 2
	}
}

// Distance returns the optimal-string-alignment distance (insertion,
// deletion, substitution, adjacent transposition) between a and b, or max+1
// as soon as it is known to exceed max.
func Distance(a, b string, max int) int {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d > max || -d > max {
		return max + 1
	}
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			v := min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				v = min(v, prev2[j-2]+1)
			}
			cur[j] = v
			rowMin = min(rowMin, v)
		}
		if rowMin > max {
			return max + 1
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(rb)]
}

// Similar reports whether a and b are within the edit budget of the shorter
// one's length (MaxEdits), after folding. Equal terms are similar.
func Similar(a, b string) bool {
	a, b = Fold(a), Fold(b)
	if a == b {
		return true
	}
	max := MaxEdits(min(len([]rune(a)), len([]rune(b))))
	return max > 0 && Distance(a, b, max) <= max
}

// Vocabulary is a term → document-frequency map with an index by length, so
// a correction only compares terms whose length is within the edit budget.
type Vocabulary struct {
	df    map[string]int
	byLen map[int][]string
}

// NewVocabulary indexes df (terms must already be folded). The map is not
// copied: callers must not modify it afterwards.
func NewVocabulary(df map[string]int) *Vocabulary {
	v := &Vocabulary{df: df, byLen: make(map[int][]string)}
	for t := range df {
		n := len([]rune(t))
		if n >= Correctable {
			v.byLen[n] = append(v.byLen[n], t)
		}
	}
	return v
}

// Size is the number of distinct terms.
func (v *Vocabulary) Size() int {
	if v == nil {
		return 0
	}
	return len(v.df)
}

const (
	// minTargetDF is the least document frequency a term needs to be offered
	// as a correction: a word seen once or twice may be a typo itself.
	minTargetDF = 3
	// dominance is how many times more frequent than the query term a
	// correction must be when the term itself is in the vocabulary.
	dominance = 8
)

// Correct returns the vocabulary term the folded term most likely meant, and
// whether it differs from the input. A term is left alone when it is too
// short, or known and not clearly outnumbered by a close neighbour.
func (v *Vocabulary) Correct(term string) (string, bool) {
	if v == nil || len(v.df) == 0 {
		return term, false
	}
	n := len([]rune(term))
	max := MaxEdits(n)
	if max == 0 {
		return term, false
	}
	own := v.df[term]
	best, bestDist, bestDF := "", max+1, 0
	for l := n - max; l <= n+max; l++ {
		for _, cand := range v.byLen[l] {
			if cand == term {
				continue
			}
			df := v.df[cand]
			if df < minTargetDF || (own > 0 && df < own*dominance) {
				continue
			}
			d := Distance(term, cand, min(max, bestDist))
			if d > max || d > bestDist {
				continue
			}
			if d < bestDist || df > bestDF {
				best, bestDist, bestDF = cand, d, df
			}
		}
	}
	if best == "" {
		return term, false
	}
	return best, true
}

// Query corrects every word of text. It returns the corrected text (words
// folded, original order, corrected ones replaced) and the substitutions made
// (original → correction); when nothing changes the text is returned as is.
func (v *Vocabulary) Query(text string) (string, map[string]string) {
	words := Words(text)
	var subs map[string]string
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w
		if c, ok := v.Correct(w); ok {
			if subs == nil {
				subs = map[string]string{}
			}
			subs[w] = c
			out[i] = c
		}
	}
	if subs == nil {
		return text, nil
	}
	return strings.Join(out, " "), subs
}
