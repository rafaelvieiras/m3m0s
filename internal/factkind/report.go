package factkind

import "fmt"

// SampleItem is one row of the hand-labelling file: the fact, the tag the
// server holds for it, and the human label to fill in (D38). Label is empty
// until a person fills it with one of All.
type SampleItem struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	CreatedAt  string `json:"createdAt"`
	Content    string `json:"content"`
	Kind       string `json:"kind"`
	KindSource string `json:"kindSource,omitempty"`
	Label      string `json:"label"`
}

// ClassStats is the agreement of one predictor on one class.
type ClassStats struct {
	Kind      string  `json:"kind"`
	Support   int     `json:"support"`   // labelled items of this class
	Predicted int     `json:"predicted"` // items the predictor put in this class
	Correct   int     `json:"correct"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
}

// Agreement summarizes how well one predictor matches the human labels.
type Agreement struct {
	Predictor string `json:"predictor"`
	Labelled  int    `json:"labelled"`
	Exact     int    `json:"exact"` // predicted == label
	// ExactPct is Exact / Labelled.
	ExactPct float64      `json:"exactPct"`
	PerClass []ClassStats `json:"perClass"`
	// Durable-vs-transient agreement is computed only over items where both
	// the label and the prediction are decided (not "other"): the question is
	// "when it takes a side, is it the right side". Coverage says how often
	// that was.
	DurableTransientCompared int     `json:"durableTransientCompared"`
	DurableTransientAgreed   int     `json:"durableTransientAgreed"`
	DurableTransientPct      float64 `json:"durableTransientPct"`
	DurableTransientCoverage float64 `json:"durableTransientCoverage"` // compared / labelled
}

// Report is the output of Evaluate.
type Report struct {
	Items     int       `json:"items"`
	Labelled  int       `json:"labelled"`
	Invalid   int       `json:"invalidLabels"`
	Stored    Agreement `json:"stored"`    // the tag the server held when the sample was exported
	Heuristic Agreement `json:"heuristic"` // the heuristic re-run on the content
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Evaluate compares the stored tag, and the heuristic recomputed on the
// content, against the human labels. Items with an empty or invalid label
// are skipped (invalid ones are counted). A stored tag that is missing reads
// as Other, like everywhere else.
//
// storedSource, when not empty, restricts the "stored" predictor to items
// whose kindSource equals it (e.g. "llm" to measure the extractor alone, not
// legacy rows or human corrections); the heuristic is always evaluated on
// every labelled item.
func Evaluate(items []SampleItem, storedSource string) Report {
	r := Report{Items: len(items)}
	var labels, stored, heur []string
	var storedLabels []string
	for _, it := range items {
		if it.Label == "" {
			continue
		}
		if !Valid(it.Label) {
			r.Invalid++
			continue
		}
		labels = append(labels, it.Label)
		if storedSource == "" || it.KindSource == storedSource {
			sk := it.Kind
			if !Valid(sk) {
				sk = Other
			}
			stored = append(stored, sk)
			storedLabels = append(storedLabels, it.Label)
		}
		hk, _ := Classify(it.Content)
		heur = append(heur, hk)
	}
	r.Labelled = len(labels)
	r.Stored = agree("stored", storedLabels, stored)
	r.Heuristic = agree("heuristic", labels, heur)
	return r
}

func agree(name string, labels, preds []string) Agreement {
	a := Agreement{Predictor: name, Labelled: len(labels)}
	support := map[string]int{}
	predicted := map[string]int{}
	correct := map[string]int{}
	for i, l := range labels {
		p := preds[i]
		support[l]++
		predicted[p]++
		if p == l {
			a.Exact++
			correct[l]++
		}
		if l != Other && p != Other {
			a.DurableTransientCompared++
			if Durable(l) == Durable(p) {
				a.DurableTransientAgreed++
			}
		}
	}
	a.ExactPct = pct(a.Exact, a.Labelled)
	a.DurableTransientPct = pct(a.DurableTransientAgreed, a.DurableTransientCompared)
	a.DurableTransientCoverage = pct(a.DurableTransientCompared, a.Labelled)
	for _, k := range All {
		if support[k] == 0 && predicted[k] == 0 {
			continue
		}
		a.PerClass = append(a.PerClass, ClassStats{
			Kind: k, Support: support[k], Predicted: predicted[k], Correct: correct[k],
			Precision: pct(correct[k], predicted[k]), Recall: pct(correct[k], support[k]),
		})
	}
	return a
}

// String renders the report for a terminal.
func (r Report) String() string {
	out := fmt.Sprintf("items: %d, labelled: %d, invalid labels: %d\n", r.Items, r.Labelled, r.Invalid)
	for _, a := range []Agreement{r.Stored, r.Heuristic} {
		out += fmt.Sprintf("\n%s tag: exact agreement %d/%d (%.1f%%)\n", a.Predictor, a.Exact, a.Labelled, a.ExactPct*100)
		out += fmt.Sprintf("  durable-vs-transient: %d/%d agree (%.1f%%), compared on %.1f%% of the labelled items\n",
			a.DurableTransientAgreed, a.DurableTransientCompared, a.DurableTransientPct*100, a.DurableTransientCoverage*100)
		out += fmt.Sprintf("  %-11s %7s %9s %9s %9s\n", "class", "support", "predicted", "precision", "recall")
		for _, c := range a.PerClass {
			out += fmt.Sprintf("  %-11s %7d %9d %8.1f%% %8.1f%%\n", c.Kind, c.Support, c.Predicted, c.Precision*100, c.Recall*100)
		}
	}
	return out
}
