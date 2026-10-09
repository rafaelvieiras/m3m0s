package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rafaelvieiras/m3m0s/internal/maintenance"
	"github.com/rafaelvieiras/m3m0s/internal/sqlstore"
)

const maintenanceUsage = `usage: m3m0s maintenance redact-scan [--data DIR] [--dry-run | --apply]
  redact-scan  run the current secret redactor over stored memories and source blobs.
               Dry run by default: reports blob hashes / entry ids and the rule ids that
               matched, never any part of a value. --apply re-redacts the source blobs
               in place; entries are only listed (rewriting them would invalidate their
               embeddings), so fix those by hand. Works on the data directory directly:
               stop "serve" first, or restart it afterwards`

// runMaintenance is `m3m0s maintenance`: offline repair jobs that act on the
// data directory rather than on a running server.
func runMaintenance(ctx context.Context, args []string) error {
	return maintenanceMain(ctx, os.Stdout, os.Stderr, args)
}

func maintenanceMain(ctx context.Context, out, errOut io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "redact-scan" {
		return errors.New(maintenanceUsage)
	}
	fs := flag.NewFlagSet("maintenance redact-scan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dataDir := fs.String("data", firstNonEmpty(os.Getenv("M3M0S_DATA_DIR"), "~/.m3m0s/data"), "directory for persisted data")
	dryRun := fs.Bool("dry-run", true, "only report (default)")
	apply := fs.Bool("apply", false, "re-redact source blobs in place")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	dryRunSet := false
	fs.Visit(func(f *flag.Flag) { dryRunSet = dryRunSet || f.Name == "dry-run" })
	if *apply && dryRunSet && *dryRun {
		return errors.New("--apply and --dry-run are mutually exclusive")
	}

	dir, err := expandHome(*dataDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "m3m0s.db")); err != nil {
		return fmt.Errorf("no m3m0s.db in %s: %w", dir, err)
	}
	db, err := sqlstore.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	rep, err := maintenance.RedactScan(ctx, db, *apply)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, rep.Format())
	return err
}
