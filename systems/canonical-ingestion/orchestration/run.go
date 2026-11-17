package orchestration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	contract "canonicalingestion"
)

type Telemetry struct {
	StagedRecords                                                            uint64 `json:"records_staged_this_process"`
	RollingAcceptedRecordsPerSecond                                          float64
	StorageAccess                                                            contract.AccessSnapshot
	Admission, AdmissionBatch                                                contract.OperationSnapshot
	BatchAdmittedRecords                                                     uint64
	CPUPercentOneCore                                                        float64
	Memory                                                                   contract.MemorySnapshot
	LocatorCache                                                             *contract.LocatorCacheSnapshot `json:",omitempty"`
	CPUWorkers, IOWorkers, MaxProcs                                          int
	PeakInputQueue, PeakPreparedQueue, PeakReorderRecords, PeakReservedBytes int64
	PeakStorageInFlight                                                      int64
	PeakCacheBytes                                                           int64
}
type child struct {
	name  string
	cmd   *exec.Cmd
	input io.WriteCloser
	log   *os.File
	path  string
}
type exit struct {
	name string
	err  error
}

// Run builds native entrypoints once, then starts all four persistent processes.
// There is no per-batch process creation and no in-memory record transport.
func Run(ctx context.Context, c Config, target uint64, out io.Writer) (runErr error) {
	return RunWithProgressFormat(ctx, c, target, out, "json")
}

// RunWithProgressFormat changes presentation only. Run retains its existing
// JSON contract for integrations; the CLI defaults to concise terminal text.
func RunWithProgressFormat(ctx context.Context, c Config, target uint64, out io.Writer, format string) (runErr error) {
	emit, err := progressEncoder(out, format)
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	mode, err := Mode(target)
	if err != nil {
		return err
	}
	if c.MVAFinalizeFirst || c.MVAPublishEachEpoch {
		if err = loadMVAFirstEnvironment(c); err != nil {
			return err
		}
	}
	if err = contract.LoadEnv(c.EnvFile); err != nil {
		return err
	}
	if (c.MVAFinalizeFirst || c.MVAPublishEachEpoch) && os.Getenv("MVA_CHECKPOINT_DSN") != "" {
		return fmt.Errorf("unset MVA_CHECKPOINT_DSN for v3 orchestration; supervisor and MVA must use the same MVA_CHECKPOINT_* settings")
	}
	db, err := open(ctx)
	if err != nil {
		return err
	}
	defer db.close()
	if err = (contract.Store{DB: db.source}).Migrate(ctx); err != nil {
		return err
	}
	if err = (contract.Store{DB: db.native}).Migrate(ctx); err != nil {
		return err
	}
	lease, err := (contract.Consumer{Store: contract.Store{DB: db.native}, ID: "orchestrator/" + c.Stream + "/" + c.RunID}).Acquire(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	initial, err := snapshot(ctx, db, c, target)
	if err != nil {
		return err
	}
	phase := ""
	if c.MVAFinalizeFirst {
		phase = "mva"
		initial.Phase = phase
	} else if c.MVAPublishEachEpoch {
		phase = "streaming"
		initial.Phase = phase
	}
	if initial.Published > 0 {
		h, e := (contract.Store{DB: db.source}).Prefix(ctx, c.Stream, initial.Published)
		if e != nil {
			return e
		}
		if fmt.Sprintf("%x", h) != initial.PrefixHash {
			return fmt.Errorf("canonical manifest prefix hash differs from stored prefix")
		}
	}
	if err = emit(initial); err != nil {
		return err
	}
	if err = os.MkdirAll(c.LogDir, 0700); err != nil {
		return err
	}
	buildRoot := filepath.Join(c.Root, "state", "orchestrator")
	if err = os.MkdirAll(buildRoot, 0700); err != nil {
		return err
	}
	binDir, err := os.MkdirTemp(buildRoot, "session-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(binDir) // Only this invocation's newly created build directory.
	type spec struct {
		name, dir, pkg string
		settings       Scheme
	}
	specs := []spec{{"delia", "delia", "./cmd/incremental_runner", c.DELIA}, {"blas", "blas", "./cmd/incremental_runner", c.BLAS}, {"dht", "dht-backed", "./benchmark/cmd/incremental_runner", c.DHT}, {"mva", "MVA-PHV", "./cmd/incremental_runner", c.MVA}}
	var children []*child
	// Preparation completes before *any* native ingestion starts. Build outputs
	// are private to this invocation; the Go cache still avoids repeated work.
	for _, s := range specs {
		name := s.name
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binary := filepath.Join(binDir, name)
		build := exec.CommandContext(ctx, "go", "build", "-p", "1", "-o", binary, s.pkg)
		build.Dir = filepath.Join(c.Root, s.dir)
		build.Stderr = out
		if err = build.Run(); err != nil {
			return fmt.Errorf("build %s: %w", s.name, err)
		}
		args := []string{"-mode", mode, "-managed", "-poll-interval", c.Poll.String(), "-cpu-workers", strconv.Itoa(s.settings.CPUWorkers), "-io-workers", strconv.Itoa(s.settings.IOWorkers), "-max-procs", strconv.Itoa(s.settings.MaxProcs), "-sql-pool-size", strconv.Itoa(s.settings.SQLPoolSize)}
		if s.name == "mva" {
			if c.MVAFinalizeFirst {
				args = append(args, "-publish-finalized-stream", c.Stream)
			} else if c.MVAPublishEachEpoch {
				args = append(args, "-publish-finalized-epochs", c.Stream, "-required-consumers", "delia/"+c.RunID+",blas/"+c.RunID+",dht/"+c.RunID, "-max-published-ahead-records", strconv.FormatUint(c.MaxAhead, 10))
			} else {
				args = append(args, "-publish-stream", c.Stream, "-required-consumers", "delia/"+c.RunID+",blas/"+c.RunID+",dht/"+c.RunID, "-max-published-ahead-records", strconv.FormatUint(c.MaxAhead, 10))
			}
		} else {
			args = append(args, "-follow", "-run", c.RunID, "-stream", c.Stream)
			if s.name == "delia" {
				args = append(args, "-key-dir", "./live-keys")
			}
		}
		keys := make([]string, 0, len(s.settings.Options))
		for k := range s.settings.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-"+k+"="+s.settings.Options[k])
		}
		cmd := exec.Command(binary, args...)
		cmd.Dir = filepath.Join(c.Root, s.dir)
		children = append(children, &child{name: s.name, cmd: cmd, path: filepath.Join(c.LogDir, s.name+".log")})
	}
	exits := make(chan exit, 4)
	live := make(map[string]*child, 4)
	var started []*child
	defer func() {
		// EOF asks each child to cancel and unwind native transactions. Death of
		// the supervisor also closes these pipes at the operating-system level.
		for _, ch := range live {
			_ = ch.input.Close()
		}
		deadline := time.NewTimer(c.ShutdownTimeout)
		defer deadline.Stop()
		for len(live) > 0 {
			select {
			case x := <-exits:
				delete(live, x.name)
			case <-deadline.C:
				for _, ch := range live {
					_ = ch.cmd.Process.Kill()
				}
				for len(live) > 0 {
					x := <-exits
					delete(live, x.name)
				}
			}
		}
		finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p, e := snapshot(finalCtx, db, c, target)
		if e == nil {
			p.Final = true
			p.Phase = phase
			attachTelemetry(&p, started)
			e = emit(p)
		}
		if e != nil {
			runErr = errors.Join(runErr, fmt.Errorf("final progress: %w", e))
		}
		for _, ch := range children {
			if ch.log != nil {
				_ = ch.log.Close()
			}
		}
	}()
	ticker := time.NewTicker(c.ProgressInterval)
	defer ticker.Stop()
	previous := initial
	groups := [][]*child{children}
	if c.MVAFinalizeFirst {
		groups = make([][]*child, 2)
		for _, ch := range children {
			group := 1
			if ch.name == "mva" {
				group = 0
			}
			groups[group] = append(groups[group], ch)
		}
	}
	for group, members := range groups {
		if c.MVAFinalizeFirst && group == 1 {
			// A zero exit alone cannot bypass the durable completion barrier.
			p, e := snapshot(ctx, db, c, target)
			if e != nil {
				return e
			}
			if p.MVAFinalized != target || p.Published < target {
				return fmt.Errorf("MVA exited before finalizing and publishing target %d; baselines were not started", target)
			}
			phase, p.Phase = "baselines", "baselines"
			if e = emit(p); e != nil {
				return e
			}
			previous = p
		}
		for _, ch := range members {
			if err = ctx.Err(); err != nil {
				return err
			}
			if (c.MVAFinalizeFirst && group == 1) || (c.MVAPublishEachEpoch && ch.name != "mva") {
				done, e := baselineAlreadyComplete(ch.name, previous)
				if e != nil {
					return e
				}
				if done {
					continue // Reuse completed baselines; never reset or reingest.
				}
			}
			ch.log, err = os.OpenFile(ch.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			// Marker prevents old-session telemetry being mistaken for current usage.
			_, _ = fmt.Fprintln(ch.log, "{\"orchestrator_session_start\":true}")
			ch.cmd.Stdout, ch.cmd.Stderr = ch.log, ch.log
			ch.input, err = ch.cmd.StdinPipe()
			if err != nil {
				return err
			}
			if err = ch.cmd.Start(); err != nil {
				ch.input.Close()
				return fmt.Errorf("start %s: %w", ch.name, err)
			}
			live[ch.name] = ch
			started = append(started, ch)
			go func(ch *child) { exits <- exit{ch.name, ch.cmd.Wait()} }(ch)
		}
		for len(live) > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case x := <-exits:
				delete(live, x.name)
				if x.err != nil {
					return fmt.Errorf("%s failed (%s): %w; committed progress retained; restart the same command", x.name, filepath.Join(c.LogDir, x.name+".log"), x.err)
				}
				if c.MVAPublishEachEpoch {
					p, e := snapshot(ctx, db, c, target)
					if e != nil {
						return e
					}
					if x.name == "mva" {
						if p.MVAFinalized != target || p.Published < target {
							return fmt.Errorf("MVA exited without finalizing/publishing target %d; stopping waiting baselines", target)
						}
						phase = "baselines"
					} else {
						done, e := baselineAlreadyComplete(x.name, p)
						if e != nil {
							return e
						}
						if !done {
							return fmt.Errorf("%s exited before completing target %d; stopping producer and waiting consumers", x.name, target)
						}
					}
				}
			case <-ticker.C:
				p, e := snapshot(ctx, db, c, target)
				if e != nil {
					return e
				}
				p.Phase = phase
				seconds := max(p.Time.Sub(previous.Time).Seconds(), 1e-9)
				p.PublishedRate = float64(p.Published-previous.Published) / seconds
				p.DELIA.Rate = float64(p.DELIA.Committed-previous.DELIA.Committed) / seconds
				p.BLAS.Rate = float64(p.BLAS.Committed-previous.BLAS.Committed) / seconds
				p.DHT.Rate = float64(p.DHT.Committed-previous.DHT.Committed) / seconds
				active := children
				if c.MVAFinalizeFirst || c.MVAPublishEachEpoch {
					active = nil
					for _, ch := range live {
						active = append(active, ch)
					}
				}
				attachTelemetry(&p, active)
				if e = emit(p); e != nil {
					return e
				}
				previous = p
			}
		}
	}
	p, err := snapshot(ctx, db, c, target)
	if err != nil {
		return err
	}
	if !p.Complete() {
		return fmt.Errorf("runners exited without complete native state for all four schemes")
	}
	return nil
}

func attachTelemetry(p *Progress, children []*child) {
	p.Children = make(map[string]Telemetry, 4)
	for _, ch := range children {
		f, err := os.Open(ch.path)
		if err != nil {
			continue
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		start := max(int64(0), st.Size()-(128<<10))
		_, _ = f.Seek(start, io.SeekStart)
		scanner := bufio.NewScanner(io.LimitReader(f, 128<<10))
		scanner.Buffer(make([]byte, 4096), 64<<10)
		var value Telemetry
		found := false
		for scanner.Scan() {
			line := scanner.Bytes()
			if strings.Contains(string(line), "orchestrator_session_start") {
				found = false
			}
			if !strings.Contains(string(line), "CPUPercentOneCore") {
				continue
			}
			if json.Unmarshal(line, &value) == nil {
				found = true
			}
		}
		f.Close()
		if found {
			p.Children[ch.name] = value
		}
	}
}
