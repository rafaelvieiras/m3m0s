package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/watch"
)

// defaultAllow/defaultDeny are the default allowlist/denylist confirmed in
// A4/D11 — only overridden if --allow/--deny are passed explicitly.
var (
	defaultAllow = []string{"AGENTS.md", "CLAUDE.md", "MEMORY.md", "docs/**/*.md"}
	defaultDeny  = []string{"node_modules/**", "dist/**", "target/**", ".git/**", "vendor/**"}
)

var domainSanitizeRe = regexp.MustCompile(`[^a-z0-9._-]+`)

// runWatch starts `m3m0s watch <dir>` — D3: watch is always an HTTP client
// of `serve`, it never touches storage directly.
func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	domain := fs.String("domain", "", "domain in the format category:instance (default: code:<root basename>)")
	statePath := fs.String("state", "", "path of the local state file (default: ~/.m3m0s/watch/<hash>.json)")
	var allow stringSlice
	fs.Var(&allow, "allow", fmt.Sprintf("glob to index (repeatable; default: %s)", strings.Join(defaultAllow, ", ")))
	var deny stringSlice
	fs.Var(&deny, "deny", fmt.Sprintf("glob to never index, always wins over --allow (repeatable; default: %s)", strings.Join(defaultDeny, ", ")))
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New(`usage: m3m0s watch <dir> [--domain category:instance] [--allow glob] [--deny glob]`)
	}

	root, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve absolute path of %q: %w", fs.Arg(0), err)
	}

	resolvedDomain := *domain
	if resolvedDomain == "" {
		resolvedDomain = "code:" + sanitizeDomainInstance(filepath.Base(root))
	}

	resolvedAllow := []string(allow)
	if len(resolvedAllow) == 0 {
		resolvedAllow = defaultAllow
	}
	resolvedDeny := []string(deny)
	if len(resolvedDeny) == 0 {
		resolvedDeny = defaultDeny
	}

	resolvedStatePath := *statePath
	if resolvedStatePath == "" {
		resolvedStatePath, err = defaultStatePath(root)
		if err != nil {
			return fmt.Errorf("resolve default state path: %w", err)
		}
	}

	state, err := watch.LoadState(resolvedStatePath)
	if err != nil {
		return fmt.Errorf("load state from %s: %w", resolvedStatePath, err)
	}

	cfg := watch.Config{
		Root:      root,
		Domain:    resolvedDomain,
		Allow:     resolvedAllow,
		Deny:      resolvedDeny,
		StatePath: resolvedStatePath,
	}

	fmt.Printf("m3m0s watch: watching %s (domain=%s)\n", root, resolvedDomain)
	// D24: timeout larger than the CLI/MCP default (30s, D17) — synchronous
	// Add with fact extraction on (D12) in batches during the initial scan can
	// legitimately take longer than an interactive call.
	return watch.Run(ctx, client.NewFromEnvWithTimeout(120*time.Second), cfg, state)
}

// sanitizeDomainInstance ensures the basename of the watched root matches
// the schema.ValidateDomain regex (^[a-z0-9_]+:[a-z0-9._-]+$ on the
// instance part): lowercase, any character outside the allowed alphabet
// becomes "-".
func sanitizeDomainInstance(name string) string {
	name = strings.ToLower(name)
	name = domainSanitizeRe.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "root"
	}
	return name
}

// defaultStatePath is ~/.m3m0s/watch/<hex(sha256(abs(dir)))[:16]>.json —
// deterministic per watched root, requiring no manual configuration to
// avoid collisions between multiple roots.
func defaultStatePath(absRoot string) (string, error) {
	home, err := expandHome("~")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(absRoot))
	dir := filepath.Join(home, ".m3m0s", "watch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".json"), nil
}
