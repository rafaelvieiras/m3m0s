package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rafaelvieiras/m3m0s/internal/mcpserver"
)

// runMCP runs the stdio<->HTTP bridge (m3m0s mcp [--read-only], D45) —
// configuration is entirely via M3M0S_URL/M3M0S_API_KEY, read by
// internal/client inside mcpserver.
func runMCP(ctx context.Context, args []string) error {
	for _, a := range args {
		if a == "--read-only" {
			mcpserver.SetReadOnly(true)
		}
	}
	if err := mcpserver.Run(ctx, os.Stdin, os.Stdout); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	return nil
}
