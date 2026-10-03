package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rafaelvieiras/m3m0s/internal/mcpserver"
)

// runMCP runs the stdio<->HTTP bridge (m3m0s mcp). No flags for now —
// configuration is entirely via M3M0S_URL/M3M0S_API_KEY, read by
// internal/client inside mcpserver.
func runMCP(ctx context.Context, args []string) error {
	if err := mcpserver.Run(ctx, os.Stdin, os.Stdout); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	return nil
}
