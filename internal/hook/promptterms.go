package hook

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/rafaelvieiras/m3m0s/internal/fuzzy"
)

// genericWords are words that carry no topic: English function words plus the
// workflow vocabulary of "do the next thing" prompts (merge, deploy, commit,
// ...). A prompt made only of these ("please merge and deploy")
// matches generic memories from any project at a high cosine (D47), so it
// gets no injection.
var genericWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
a an the and or but if then than so as at by for from in into of on to with without about over under
i you he she it we they me my your his her its our their this that these those there here
is are was were be been being am do does did done can could should would will shall may might must have has had
what which who whom whose when where why how not no yes ok okay
please go ahead next continue proceed start begin run make let lets us now look see check just also first last
more most less very much many some any all each every other another same such only own too
merge deploy commit push pull request branch build test tests fix review validate verify
done finish finished ready item items step steps thing things part parts way case time times
`) {
		m[w] = true
	}
	for _, w := range extraStopwords() {
		m[w] = true
	}
	return m
}()

// extraStopwords reads optional extra generic words (any language) from the
// file named by M3M0S_PROMPT_STOPWORDS, or ~/.m3m0s/prompt-stopwords.txt:
// whitespace-separated, accents folded. The built-in list is English only.
func extraStopwords() []string {
	path := os.Getenv("M3M0S_PROMPT_STOPWORDS")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".m3m0s", "prompt-stopwords.txt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Fields(foldText(string(data)))
}

// foldAccents maps the accented letters of Portuguese to ASCII so terms
// match regardless of accents.
var foldAccents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "î", "i", "ì", "i", "ï", "i",
	"ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
	"ú", "u", "û", "u", "ù", "u", "ü", "u", "ç", "c", "ñ", "n",
)

func foldText(s string) string { return foldAccents.Replace(strings.ToLower(s)) }

// specificTerms returns the topic-bearing terms of a prompt: lowercase,
// accent-folded words of 3+ characters (or any word with a digit, like "f3"
// or "d47") that are not generic. Duplicates are removed.
func specificTerms(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.FieldsFunc(foldText(prompt), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	}) {
		w = strings.Trim(w, "-_")
		hasDigit := strings.IndexFunc(w, unicode.IsDigit) >= 0
		if (len([]rune(w)) < 3 && !hasDigit) || genericWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// sharesTerm reports whether content mentions at least one of the terms,
// exactly or - for a misspelled term (D51) - as a word within the edit
// budget of fuzzy.MaxEdits, so "playtheadtv" still ties a prompt to a memory
// about "playheadtv".
func sharesTerm(content string, terms []string) bool {
	c := foldText(content)
	for _, t := range terms {
		if strings.Contains(c, t) {
			return true
		}
	}
	words := fuzzy.Words(content)
	for _, t := range terms {
		for _, w := range words {
			if fuzzy.Similar(t, w) {
				return true
			}
		}
	}
	return false
}
