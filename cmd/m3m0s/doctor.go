package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/hook"
	"github.com/rafaelvieiras/m3m0s/internal/project"
)

// claudeHookEvents are the Claude Code events the capture path relies on.
var claudeHookEvents = []string{"SessionStart", "PreCompact", "SessionEnd", "Stop"}

// claudeOptionalHookEvents are cheap safety-net events (a stat until the
// transcript grows); recommended, not required.
var claudeOptionalHookEvents = []string{"UserPromptSubmit", "PostToolUse"}

// runDoctor is `m3m0s doctor`: it checks everything capture depends on and
// prints actionable output. Hooks always exit 0 (they must never break the
// host agent), so this is where failures become visible. Exit status is 1
// when any check fails.
func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	replay := fs.Bool("replay", false, "resend the pending spool to the server")
	if err := fs.Parse(args); err != nil {
		return err
	}
	home, err := expandHome("~")
	if err != nil {
		return err
	}

	failed := false
	report := func(ok bool, format string, a ...any) {
		mark := "ok  "
		if !ok {
			mark, failed = "FAIL", true
		}
		fmt.Printf("[%s] %s\n", mark, fmt.Sprintf(format, a...))
	}
	note := func(format string, a ...any) { fmt.Printf("       %s\n", fmt.Sprintf(format, a...)) }
	info := func(format string, a ...any) { fmt.Printf("[info] %s\n", fmt.Sprintf(format, a...)) }

	baseURL, apiKey, ok := hook.ResolveCredentials(os.Getenv, home)
	var c *client.Client
	if !ok {
		report(false, "credentials: M3M0S_API_KEY not found (environment or ~/.m3m0s/hook.env)")
		note("fix: create ~/.m3m0s/hook.env with M3M0S_URL=... and M3M0S_API_KEY=... (mode 600)")
	} else {
		report(true, "credentials: %s", baseURL)
		c = client.NewWithTimeout(baseURL, apiKey, 10*time.Second)
		if err := c.Health(ctx); err != nil {
			report(false, "server: %v", err)
			note("fix: start/check the server; captures meanwhile wait in the spool")
			c = nil
		} else {
			report(true, "server: /health answered")
			if _, err := c.Stats(ctx); err != nil {
				report(false, "api key: rejected or API error: %v", err)
				note("fix: check M3M0S_API_KEY against the server's keys")
			} else {
				report(true, "api key: accepted")
			}
		}
	}

	checkClaudeHooks(home, report, note, info)
	checkMCPClients(home, info)

	st := hook.Stats(home)
	if *replay && st.Pending > 0 && c != nil {
		res := hook.Replay(ctx, home, c, 0, time.Now())
		fmt.Printf("       replay: sent=%d failed=%d dead=%d expired=%d unreachable=%v\n", res.Sent, res.Failed, res.Dead, res.Expired, res.Unreachable)
		st = hook.Stats(home)
	}
	switch {
	case st.Pending == 0 && st.Dead == 0:
		report(true, "spool: empty")
	default:
		report(st.Pending == 0, "spool: %d pending (%d KB), oldest %s; %d dead-lettered", st.Pending, st.Bytes/1024, ageOf(st.Oldest), st.Dead)
		if st.Pending > 0 {
			note("fix: run `m3m0s doctor --replay` (pending are also retried by the next capture hook)")
		}
		if st.Dead > 0 {
			note("dead-lettered payloads were rejected %d times by the server: inspect %s", 5, filepath.Join(hook.SpoolDir(home), "dead"))
		}
	}

	if m, err := project.Load(home); err != nil {
		report(false, "projects: %v", err)
	} else {
		cwd, _ := os.Getwd()
		if p := project.Resolve(os.Getenv, home, cwd); p != "" {
			info("project: %s (%d directories mapped)", p, len(m))
		} else {
			info("project: none for the current directory (%d directories mapped)", len(m))
		}
	}

	errs, last := recentHookFailures(filepath.Join(home, ".m3m0s", "hooks", "claude-code.log"), 24*time.Hour)
	if errs == 0 {
		report(true, "hook log: no failures in the last 24h")
	} else {
		report(false, "hook log: %d failure line(s) in the last 24h; last: %s", errs, last)
	}

	if failed {
		return errors.New("doctor found problems")
	}
	return nil
}

func ageOf(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return time.Since(t).Round(time.Minute).String() + " ago"
}

// checkClaudeHooks verifies ~/.claude/settings.json invokes
// `m3m0s hook claude-code` for each event capture needs.
func checkClaudeHooks(home string, report func(bool, string, ...any), note func(string, ...any), info func(string, ...any)) {
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		report(false, "claude hooks: ~/.claude/settings.json not readable (skip if you do not use Claude Code)")
		return
	}
	var settings struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		report(false, "claude hooks: settings.json is not valid JSON: %v", err)
		return
	}
	var missing []string
	for _, ev := range claudeHookEvents {
		if !strings.Contains(string(settings.Hooks[ev]), "hook claude-code") {
			missing = append(missing, ev)
		}
	}
	if len(missing) > 0 {
		report(false, "claude hooks: missing for %s", strings.Join(missing, ", "))
		note(`fix: add a command hook running "m3m0s hook claude-code" to those events`)
		return
	}
	report(true, "claude hooks: installed for %s", strings.Join(claudeHookEvents, ", "))
	var optional []string
	for _, ev := range claudeOptionalHookEvents {
		if !strings.Contains(string(settings.Hooks[ev]), "hook claude-code") {
			optional = append(optional, ev)
		}
	}
	if len(optional) > 0 {
		info("claude hooks: optional safety-net events not installed: %s", strings.Join(optional, ", "))
	}
}

// checkMCPClients reports (informationally: a client you do not use is not a
// failure) whether each known MCP host has an m3m0s server configured.
func checkMCPClients(home string, info func(string, ...any)) {
	type clientCfg struct {
		name, path, key string
	}
	for _, c := range []clientCfg{
		{"claude code", filepath.Join(home, ".claude.json"), "mcpServers"},
		{"opencode", filepath.Join(home, ".config", "opencode", "opencode.json"), "mcp"},
	} {
		data, err := os.ReadFile(c.path)
		if err != nil {
			continue // client not installed
		}
		var cfg map[string]json.RawMessage
		if json.Unmarshal(data, &cfg) != nil {
			info("mcp (%s): %s is not valid JSON", c.name, c.path)
			continue
		}
		if strings.Contains(string(cfg[c.key]), "m3m0s") {
			info("mcp (%s): m3m0s configured", c.name)
		} else {
			info("mcp (%s): m3m0s MCP server not configured in %s", c.name, c.path)
		}
	}
}

// recentHookFailures counts timestamped log lines newer than window that
// look like failures (anything not a plain informational line).
func recentHookFailures(path string, window time.Duration) (int, string) {
	f, err := os.Open(path)
	if err != nil {
		return 0, ""
	}
	defer func() { _ = f.Close() }()
	cutoff := time.Now().Add(-window)
	var n int
	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		ts, ok := hook.ParseLogTime(line)
		if !ok || ts.Before(cutoff) {
			continue
		}
		if isInfoLogLine(line) {
			continue
		}
		n++
		last = line
	}
	return n, last
}

// isInfoLogLine reports whether a timestamped hook log line is informational
// (a successful SessionStart injection) and not a failure.
func isInfoLogLine(line string) bool {
	return strings.Contains(line, "hook claude-code: injected ")
}
