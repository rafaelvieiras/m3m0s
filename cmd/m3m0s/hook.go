package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rafaelvieiras/m3m0s/internal/client"
	"github.com/rafaelvieiras/m3m0s/internal/hook"
)

// runHook dispatches `m3m0s hook <tool>` — today only "claude-code" (Phase
// 8, D14). Kept separate from the main dispatcher in main.go to leave room
// for other hook tools without reopening the switch there.
func runHook(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New(`usage: m3m0s hook <tool>, e.g. m3m0s hook claude-code`)
	}
	switch args[0] {
	case "claude-code":
		return runHookClaudeCode(ctx)
	default:
		return fmt.Errorf("unknown hook tool: %s", args[0])
	}
}

// runHookClaudeCode is `m3m0s hook claude-code` (Phase 8, D14; D17): it
// reads the Claude Code hook payload via stdin and dispatches by
// hook_event_name — on PreCompact/SessionEnd, it reads the transcript and
// forwards new content to `m3m0s serve` (D14); on SessionStart, it fetches
// recent memories of the current domain and writes to stdout the JSON that
// Claude Code injects as additional context into the session (D17).
// Deliberately always returns nil — hook.Run already handles every failure
// internally as a stderr log + silent return (the hook must never hang or
// appear to fail the user's Claude Code session, even if m3m0s is down or
// credentials are missing).
func runHookClaudeCode(ctx context.Context) error {
	home, err := expandHome("~")
	if err != nil {
		fmt.Fprintf(os.Stderr, "m3m0s hook claude-code: resolve home dir: %v\n", err)
		return nil
	}

	deps := hook.Deps{
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		HomeDir:   home,
		Getenv:    os.Getenv,
		Domain:    hookDomainFromCwd,
		NewClient: client.New,
	}
	hook.Run(ctx, os.Stdin, deps)
	return nil
}

// hookDomainFromCwd derives codigo:<basename(cwd)> — the same
// sanitizeDomainInstance rule already used by `m3m0s watch` (D11), reused
// here without duplication (D14, item 4: same domain inference across the
// project's two automatic capture mechanisms).
func hookDomainFromCwd(cwd string) string {
	return "codigo:" + sanitizeDomainInstance(filepath.Base(cwd))
}
