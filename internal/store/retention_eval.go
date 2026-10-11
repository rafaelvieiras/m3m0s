package store

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/factkind"
	"github.com/rafaelvieiras/m3m0s/internal/schema"
)

// Offline evaluation of the retention models against a hand-labelled sample
// (the file written by `m3m0s kind sample`, labelled by a person). It answers
// the acceptance question of issue #52: on the labelled sample, how many
// DURABLE facts would each model forget within the horizon? It needs no
// server and touches no data.

// SampleKinds says which kind the evaluation gives each item.
const (
	// SampleKindsStored uses the tag the sample carries (and its kindSource),
	// i.e. what the server would act on.
	SampleKindsStored = "stored"
	// SampleKindsLabel uses the human label as an LLM-sourced tag: the best a
	// perfect classifier could do, to separate the score's behaviour from the
	// classifier's accuracy.
	SampleKindsLabel = "label"
)

// RetentionSampleOptions configures EvaluateRetentionSample.
type RetentionSampleOptions struct {
	HorizonDays int       // how far ahead to look (30 in the acceptance criterion)
	Now         time.Time // the "today" the ages are measured from
	Kinds       string    // SampleKindsStored (default) or SampleKindsLabel
}

// ModelSampleResult is one model's outcome on the sample.
type ModelSampleResult struct {
	Model              RetentionModel `json:"model"`
	Forgotten          int            `json:"forgotten"`
	DurableLabelled    int            `json:"durableLabelled"`
	DurableForgotten   int            `json:"durableForgotten"`
	DurableForgottenID []string       `json:"durableForgottenIds,omitempty"`
	TransientLabelled  int            `json:"transientLabelled"`
	TransientForgotten int            `json:"transientForgotten"`
}

// RetentionSampleReport is the output of EvaluateRetentionSample.
type RetentionSampleReport struct {
	HorizonDays int                 `json:"horizonDays"`
	Kinds       string              `json:"kinds"`
	Items       int                 `json:"items"`
	Labelled    int                 `json:"labelled"`
	Skipped     int                 `json:"skipped"` // unlabelled, invalid label or unreadable date
	Results     []ModelSampleResult `json:"results"`
}

// EvaluateRetentionSample scores every labelled item at Now+HorizonDays under
// both models, assuming no access (the worst case for forgetting) and no
// links, and counts what each would forget by the human label. Every item is
// treated as an extracted, unpinned fact, which is what the sample holds.
func EvaluateRetentionSample(items []factkind.SampleItem, p RetentionParams, o RetentionSampleOptions) RetentionSampleReport {
	if o.Kinds == "" {
		o.Kinds = SampleKindsStored
	}
	at := o.Now.Add(time.Duration(o.HorizonDays) * 24 * time.Hour)
	rep := RetentionSampleReport{HorizonDays: o.HorizonDays, Kinds: o.Kinds, Items: len(items)}
	res := map[RetentionModel]*ModelSampleResult{
		RetentionLegacy:  {Model: RetentionLegacy},
		RetentionPerKind: {Model: RetentionPerKind},
	}
	for _, it := range items {
		if it.Label == "" || !factkind.Valid(it.Label) {
			rep.Skipped++
			continue
		}
		created, err := time.Parse(time.RFC3339, it.CreatedAt)
		if err != nil {
			rep.Skipped++
			continue
		}
		rep.Labelled++
		md := map[string]string{metadataKeySourceContentRef: "sample"}
		switch o.Kinds {
		case SampleKindsLabel:
			md[factkind.MetadataKey], md[factkind.MetadataSourceKey] = it.Label, factkind.SourceLLM
		default:
			if it.Kind != "" {
				md[factkind.MetadataKey] = it.Kind
			}
			if it.KindSource != "" {
				md[factkind.MetadataSourceKey] = it.KindSource
			}
		}
		e := schema.Entry{ID: it.ID, CreatedAt: created, UpdatedAt: created, Metadata: md}
		index := map[string]schema.Entry{e.ID: e}
		for _, model := range []RetentionModel{RetentionLegacy, RetentionPerKind} {
			ev := newRetentionEval(index, model, p)
			r := res[model]
			forgotten := ev.eligible(e) && ev.cold(e, at)
			if factkind.Durable(it.Label) {
				r.DurableLabelled++
			}
			if factkind.Transient(it.Label) {
				r.TransientLabelled++
			}
			if !forgotten {
				continue
			}
			r.Forgotten++
			if factkind.Durable(it.Label) {
				r.DurableForgotten++
				r.DurableForgottenID = append(r.DurableForgottenID, it.ID)
			}
			if factkind.Transient(it.Label) {
				r.TransientForgotten++
			}
		}
	}
	for _, model := range []RetentionModel{RetentionLegacy, RetentionPerKind} {
		sort.Strings(res[model].DurableForgottenID)
		rep.Results = append(rep.Results, *res[model])
	}
	return rep
}

// String renders the report for a terminal.
func (r RetentionSampleReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "retention on %d labelled facts (%d skipped), horizon +%d days, kinds: %s\n",
		r.Labelled, r.Skipped, r.HorizonDays, r.Kinds)
	for _, m := range r.Results {
		fmt.Fprintf(&b, "  %-9s forgets %d; durable forgotten %d of %d; transient forgotten %d of %d\n",
			m.Model, m.Forgotten, m.DurableForgotten, m.DurableLabelled, m.TransientForgotten, m.TransientLabelled)
		if len(m.DurableForgottenID) > 0 {
			fmt.Fprintf(&b, "            durable ids: %s\n", strings.Join(m.DurableForgottenID, ", "))
		}
	}
	return b.String()
}
