package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite" // driver, also linked in through internal/sqlstore

	"github.com/rafaelvieiras/m3m0s/internal/ingestfilter"
	"github.com/rafaelvieiras/m3m0s/internal/redact"
)

const filterUsage = `usage: m3m0s filter replay [flags]
  replay   runs the ingestion boilerplate rules over stored text and reports the hits per rule,
           without writing anything (the database is opened read-only)

flags:
  -data DIR            data directory (or the path of m3m0s.db) to sample facts and source texts from
  -facts FILE          text file, one fact per line
  -segments FILE       text file holding one captured digest ("[user]\n..." messages)
  -rules/-mode/-modes  same as the server (M3M0S_FILTER_RULES, M3M0S_FILTER_MODE, M3M0S_FILTER_MODES)
  -examples N          print up to N redacted, truncated examples per rule (default 0: counts only)
  -json                machine-readable output`

// loadFilterEngine builds the engine from the three operator settings.
func loadFilterEngine(rules, mode, modes string) (*ingestfilter.Engine, error) {
	rm, err := ingestfilter.ParseModes(modes)
	if err != nil {
		return nil, err
	}
	return ingestfilter.Load(ingestfilter.Config{RulesFile: rules, Mode: mode, RuleModes: rm})
}

func runFilter(ctx context.Context, args []string) error {
	return filterMain(ctx, os.Stdout, os.Stderr, args)
}

// ReplayReport is the output of `m3m0s filter replay`.
type replayReport struct {
	Mode         string                   `json:"mode"`
	RulesVersion int                      `json:"rules_version"`
	Facts        int                      `json:"facts_examined"`
	Segments     int                      `json:"segments_examined"`
	Rules        []ingestfilter.RuleStats `json:"rules"`
	Examples     map[string][]string      `json:"examples,omitempty"`
}

func filterMain(ctx context.Context, out, errOut io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "replay" {
		return errors.New(filterUsage)
	}
	fs := flag.NewFlagSet("filter replay", flag.ContinueOnError)
	fs.SetOutput(errOut)
	data := fs.String("data", "", "data directory or m3m0s.db path")
	factsFile := fs.String("facts", "", "file with one fact per line")
	segFile := fs.String("segments", "", "file with one captured digest")
	rules := fs.String("rules", os.Getenv("M3M0S_FILTER_RULES"), "rules file")
	mode := fs.String("mode", os.Getenv("M3M0S_FILTER_MODE"), "global mode")
	modes := fs.String("modes", os.Getenv("M3M0S_FILTER_MODES"), "per-rule modes")
	examples := fs.Int("examples", 0, "examples per rule")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *data == "" && *factsFile == "" && *segFile == "" {
		return errors.New("nothing to replay: pass -data, -facts or -segments\n" + filterUsage)
	}
	eng, err := loadFilterEngine(*rules, *mode, *modes)
	if err != nil {
		return err
	}

	rep := replayReport{Examples: map[string][]string{}}
	note := func(d ingestfilter.Decision, text string) {
		if !d.Matched() || *examples <= 0 || len(rep.Examples[d.RuleID]) >= *examples {
			return
		}
		rep.Examples[d.RuleID] = append(rep.Examples[d.RuleID], exampleText(text))
	}
	fact := func(text string) {
		rep.Facts++
		note(eng.CheckFact(text), text)
	}
	digest := func(text string) {
		for _, sg := range ingestfilter.SplitSegments(text) {
			rep.Segments++
			body := strings.TrimSpace(sg.Body)
			note(eng.Check(ingestfilter.ScopeSegment, body), body)
		}
	}

	if *factsFile != "" {
		if err := eachLine(*factsFile, fact); err != nil {
			return err
		}
	}
	if *segFile != "" {
		b, err := os.ReadFile(*segFile)
		if err != nil {
			return err
		}
		digest(string(b))
	}
	if *data != "" {
		if err := replayDB(ctx, *data, fact, digest); err != nil {
			return err
		}
	}

	st := eng.Stats()
	rep.Mode, rep.RulesVersion, rep.Rules = st.Mode, st.Version, st.Rules
	if *jsonOut {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(b))
		return err
	}
	_, _ = fmt.Fprintf(out, "mode=%s rules=v%d facts examined=%d segments examined=%d (nothing was written)\n\n",
		rep.Mode, rep.RulesVersion, rep.Facts, rep.Segments)
	_, _ = fmt.Fprintf(out, "%-32s %-8s %-6s %8s %8s %8s\n", "rule", "scope", "action", "seen", "dropped", "tagged")
	for _, r := range rep.Rules {
		_, _ = fmt.Fprintf(out, "%-32s %-8s %-6s %8d %8d %8d\n", r.ID, r.Scope, r.Action, r.Seen, r.Dropped, r.Tagged)
	}
	for id, ex := range rep.Examples {
		_, _ = fmt.Fprintf(out, "\n%s examples:\n", id)
		for _, e := range ex {
			_, _ = fmt.Fprintf(out, "  - %s\n", e)
		}
	}
	return nil
}

// exampleText is a redacted, single-line, truncated view of text.
func exampleText(s string) string {
	s = redact.Redact(strings.Join(strings.Fields(s), " ")).Text
	if utf8.RuneCountInString(s) > 120 {
		s = string([]rune(s)[:120]) + "..."
	}
	return s
}

func eachLine(path string, fn func(string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			fn(line)
		}
	}
	return sc.Err()
}

// replayDB streams the active entries' text (as facts) and the stored source
// texts (as digests) from a read-only connection.
func replayDB(ctx context.Context, data string, fact, digest func(string)) error {
	path := data
	if st, err := os.Stat(data); err == nil && st.IsDir() {
		path = filepath.Join(data, "m3m0s.db")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, `SELECT content, metadata_json FROM entries`)
	if err != nil {
		return fmt.Errorf("read entries: %w", err)
	}
	for rows.Next() {
		var content, mdJSON string
		if err := rows.Scan(&content, &mdJSON); err != nil {
			_ = rows.Close()
			return err
		}
		var md map[string]string
		if json.Unmarshal([]byte(mdJSON), &md) == nil {
			if md["deleted_at"] != "" || md["pinned"] == "true" {
				continue // soft-deleted and pinned entries are never filtered
			}
		}
		fact(content)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_ = rows.Close()

	rows, err = db.QueryContext(ctx, `SELECT content FROM source_blobs`)
	if err != nil {
		return fmt.Errorf("read source texts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return err
		}
		digest(content)
	}
	return rows.Err()
}
