package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	contract "canonicalingestion"
	"canonicalingestion/orchestration"
)

const maxIngestionRestarts uint64 = 3

// Retry the ingestion supervisor, not individual native operations. Each failed
// attempt first unwinds RunWithProgressFormat: child processes are stopped and
// dataset leases/connections are released before the delay begins. The same
// parsed profile, target and native flags are used on every attempt; durable
// journals/cursors remain authoritative. No experiment/audit path calls this.
func ingestWithRestart(ctx context.Context, config orchestration.Config, target uint64, out io.Writer, format string, restart bool, delay time.Duration, profile string, arguments []string) error {
	for attempt := uint64(1); ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := orchestration.RunWithProgressFormat(ctx, config, target, out, format)
		if ctx.Err() != nil {
			return ctx.Err() // An operator stop must never restart the workers.
		}
		if err == nil || !restart {
			return err
		}
		if attempt > maxIngestionRestarts {
			return fmt.Errorf("ingestion stopped after %d automatic restarts (%d attempts): %w", maxIngestionRestarts, attempt, err)
		}
		now := time.Now().UTC()
		var writeErr error
		if format == "json" {
			writeErr = json.NewEncoder(out).Encode(struct {
				Type         string    `json:"type"`
				Time         time.Time `json:"time"`
				Attempt      uint64    `json:"failed_attempt"`
				Restart      uint64    `json:"restart_number"`
				RestartLimit uint64    `json:"restart_limit"`
				Target       uint64    `json:"target"`
				Profile      string    `json:"config"`
				Arguments    []string  `json:"arguments"`
				DelaySeconds float64   `json:"restart_delay_seconds"`
				RestartAt    time.Time `json:"restart_at"`
				Error        string    `json:"error"`
			}{"ingestion_restart_wait", now, attempt, attempt, maxIngestionRestarts, target, profile, arguments, delay.Seconds(), now.Add(delay), err.Error()})
		} else {
			_, writeErr = fmt.Fprintf(out, "[%s] INGESTION STOPPED attempt=%d: %v\n  Restart %d/%d in %s at %s with the same target=%d config=%s and native flags. Durable progress is retained; Ctrl+C cancels.\n", now.Local().Format("15:04:05"), attempt, err, attempt, maxIngestionRestarts, delay, now.Add(delay).Local().Format("15:04:05"), target, profile)
		}
		if writeErr != nil {
			return errors.Join(err, fmt.Errorf("write ingestion restart status: %w", writeErr))
		}
		if err = contract.Wait(ctx, delay); err != nil {
			return err
		}
	}
}
