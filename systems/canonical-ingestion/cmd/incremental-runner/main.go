package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	contract "canonicalingestion"
	"canonicalingestion/orchestration"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 || args[0] != "ingest" {
		return fmt.Errorf("usage: incremental-runner ingest --target 10000000 --config comparison.yaml")
	}
	f := flag.NewFlagSet("ingest", flag.ContinueOnError)
	target := f.Uint64("target", 0, "one cumulative milestone, without resetting existing state")
	path := f.String("config", "comparison.yaml", "shared orchestration profile")
	progressFormat := f.String("progress-format", "text", "terminal progress: text (concise) or json (full metrics)")
	restart := f.Bool("restart-on-failure", true, "restart failed ingestion at most 3 times with the same target/profile; Ctrl+C cancels")
	restartDelay := f.Duration("restart-delay", 60*time.Second, "wait after failed ingestion cleanup before restarting")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *restartDelay <= 0 {
		return fmt.Errorf("restart-delay must be positive")
	}
	if *progressFormat != "text" && *progressFormat != "json" {
		return fmt.Errorf("invalid progress format %q: use text or json", *progressFormat)
	}
	if _, err := orchestration.Mode(*target); err != nil {
		return err
	}
	c, err := orchestration.Load(*path)
	if err != nil {
		return err
	}
	ctx, stop := contract.ManagedContext(false)
	defer stop()
	return ingestWithRestart(ctx, c, *target, os.Stdout, *progressFormat, *restart, *restartDelay, *path, append([]string(nil), args...))
}
