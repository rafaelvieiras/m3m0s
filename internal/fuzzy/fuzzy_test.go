package fuzzy

import (
	"reflect"
	"testing"
)

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		max  int
		want int
	}{
		{"kitten", "kitten", 2, 0},
		{"kitten", "sitten", 2, 1},
		{"kitten", "sitting", 3, 3},
		{"kitten", "sitting", 2, 3}, // over budget: max+1
		{"ab", "ba", 1, 1},          // adjacent transposition is one edit
		{"gateway", "gatewya", 1, 1},
		{"short", "muchlongerword", 2, 3}, // length gap alone exceeds the budget
	}
	for _, c := range cases {
		if got := Distance(c.a, c.b, c.max); got != c.want {
			t.Errorf("Distance(%q,%q,%d) = %d, want %d", c.a, c.b, c.max, got, c.want)
		}
	}
}

func TestFoldAndWords(t *testing.T) {
	if got := Fold("Über NAÏVE façade"); got != "uber naive facade" {
		t.Errorf("Fold = %q", got)
	}
	got := Words("Hello, wörld-wide  web_2!")
	want := []string{"hello", "world", "wide", "web", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Words = %v, want %v", got, want)
	}
}

func TestMaxEdits(t *testing.T) {
	for n, want := range map[int]int{1: 0, 3: 0, 4: 0, 5: 1, 7: 1, 8: 2, 20: 2} {
		if got := MaxEdits(n); got != want {
			t.Errorf("MaxEdits(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestSimilar(t *testing.T) {
	yes := [][2]string{{"gateway", "gatewya"}, {"kubernetes", "kubernetse"}, {"Über", "uber"}, {"deployment", "deploymnt"}}
	no := [][2]string{{"cat", "cap"}, {"deploy", "destroy"}, {"alpha", "omega"}, {"test", "tent"}}
	for _, p := range yes {
		if !Similar(p[0], p[1]) {
			t.Errorf("Similar(%q,%q) = false", p[0], p[1])
		}
	}
	for _, p := range no {
		if Similar(p[0], p[1]) {
			t.Errorf("Similar(%q,%q) = true", p[0], p[1])
		}
	}
}

func testVocabulary() *Vocabulary {
	return NewVocabulary(map[string]int{
		"gateway":    120,
		"kubernetes": 80,
		"database":   200,
		"databases":  40,
		"cache":      15,
		"cachet":     1, // a one-off, probably itself a typo
		"gatewya":    1, // a typo that was captured once
		"orchard":    4,
		"ab":         900,
	})
}

func TestCorrect(t *testing.T) {
	v := testVocabulary()
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"gatewya", "gateway", true},       // known once, but far rarer than its neighbour
		{"kuberentes", "kubernetes", true}, // transposition
		{"kubernetse", "kubernetes", true},
		{"gateway", "gateway", false},     // exact, frequent
		{"databases", "databases", false}, // frequent, close to "database" but not outnumbered 8x
		{"orchard", "orchard", false},     // known and rare, nothing dominant nearby
		{"zebra", "zebra", false},         // unknown, nothing near
		{"cat", "cat", false},             // too short to correct
		{"cachet", "cache", true},         // one-off replaced by the common neighbour
	}
	for _, c := range cases {
		got, ok := v.Correct(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Correct(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestCorrectWithoutVocabulary(t *testing.T) {
	var v *Vocabulary
	if got, ok := v.Correct("anything"); ok || got != "anything" {
		t.Errorf("nil vocabulary corrected: %q %v", got, ok)
	}
	if got, subs := v.Query("keep this text"); got != "keep this text" || subs != nil {
		t.Errorf("nil vocabulary query: %q %v", got, subs)
	}
}

func TestQuery(t *testing.T) {
	v := testVocabulary()
	got, subs := v.Query("What about the gatewya and kuberentes setup?")
	if got != "what about the gateway and kubernetes setup" {
		t.Errorf("corrected = %q", got)
	}
	want := map[string]string{"gatewya": "gateway", "kuberentes": "kubernetes"}
	if !reflect.DeepEqual(subs, want) {
		t.Errorf("subs = %v", subs)
	}
	if text, subs := v.Query("Nothing to fix here, gateway"); subs != nil || text != "Nothing to fix here, gateway" {
		t.Errorf("unchanged query modified: %q %v", text, subs)
	}
}
