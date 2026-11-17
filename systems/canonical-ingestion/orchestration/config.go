// Package orchestration supervises the four existing native runners. It owns
// no authentication algorithm and never writes a native progress cursor.
package orchestration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	contract "canonicalingestion"
	"gopkg.in/yaml.v3"
)

type Scheme struct {
	CPUWorkers  int `yaml:"cpu_workers"`
	IOWorkers   int `yaml:"io_workers"`
	MaxProcs    int `yaml:"max_procs"`
	SQLPoolSize int `yaml:"sql_pool_size"`
	// Existing native and execution flags, excluding supervisor-owned identity,
	// target and flow-control flags. Native defaults remain the existing CLI's.
	Options map[string]string `yaml:"options"`
}
type Config struct {
	Root                string        `yaml:"root"`
	EnvFile             string        `yaml:"env_file"`
	Stream              string        `yaml:"stream"`
	RunID               string        `yaml:"run_id"`
	MaxAhead            uint64        `yaml:"max_published_ahead_records"`
	Poll                time.Duration `yaml:"poll_interval"`
	ProgressInterval    time.Duration `yaml:"progress_interval"`
	ShutdownTimeout     time.Duration `yaml:"shutdown_timeout"`
	LogDir              string        `yaml:"log_dir"`
	MVAFinalizeFirst    bool          `yaml:"mva_finalize_first"`
	MVAPublishEachEpoch bool          `yaml:"mva_publish_each_epoch"`
	MVA                 Scheme        `yaml:"mva"`
	DELIA               Scheme        `yaml:"delia"`
	BLAS                Scheme        `yaml:"blas"`
	DHT                 Scheme        `yaml:"dht"`
}

func Defaults() Config {
	base := Scheme{CPUWorkers: 3, IOWorkers: 8, MaxProcs: 4, SQLPoolSize: 6}
	mva := base
	mva.CPUWorkers, mva.IOWorkers = 4, 12
	return Config{Root: ".", EnvFile: "MVA-PHV/.env.local", Stream: "canonical-v1", RunID: "comparison-v1", MaxAhead: 1000000, Poll: 500 * time.Millisecond, ProgressInterval: 5 * time.Second, ShutdownTimeout: 30 * time.Second, LogDir: "results/logs/orchestrator", MVA: mva, DELIA: base, BLAS: base, DHT: base}
}
func Load(path string) (Config, error) {
	c := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, fmt.Errorf("one YAML document required")
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return c, err
	}
	if !filepath.IsAbs(c.Root) {
		c.Root = filepath.Join(base, c.Root)
	}
	for _, p := range []*string{&c.EnvFile, &c.LogDir} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(c.Root, *p)
		}
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.Stream == "" || c.RunID == "" || strings.Contains(c.RunID, ",") || c.MaxAhead < nativeEpochSize || c.Poll <= 0 || c.ProgressInterval <= 0 || c.ShutdownTimeout <= 0 {
		return fmt.Errorf("stream/run identity, lead >= native epoch (%d), positive intervals required", nativeEpochSize)
	}
	if c.MVAFinalizeFirst && c.MVAPublishEachEpoch {
		return fmt.Errorf("choose per-epoch MVA publication or whole-target MVA-first execution, not both")
	}
	if (c.MVAFinalizeFirst || c.MVAPublishEachEpoch) && c.MVA.Options["config"] == "" {
		return fmt.Errorf("v3 orchestration requires an explicit MVA logical-aging-v3 config")
	}
	for _, s := range []Scheme{c.MVA, c.DELIA, c.BLAS, c.DHT} {
		e := contract.DefaultExecution()
		e.CPUWorkers = s.CPUWorkers
		e.IOWorkers = s.IOWorkers
		e.MaxProcs = s.MaxProcs
		e.SQLPoolSize = s.SQLPoolSize
		if err := e.Validate(); err != nil {
			return err
		}
		for k := range s.Options {
			if strings.HasPrefix(k, "-") || strings.ContainsAny(k, "= \r\n") {
				return fmt.Errorf("invalid option name %q", k)
			}
			switch k {
			case "mode", "run", "stream", "follow", "managed", "poll-interval", "env-file", "publish-stream", "publish-finalized-stream", "publish-finalized-epochs", "required-consumers", "max-published-ahead-records", "prepare", "prepare-keys", "cpu-workers", "io-workers", "max-procs", "sql-pool-size":
				return fmt.Errorf("option %q is owned by the supervisor", k)
			}
		}
	}
	return nil
}

// The 10K integration uses a Go source overlay with a 1K test-only native epoch.
// Production binaries retain the original 100K journal/checkpoint semantics.
const nativeEpochSize uint64 = 100000

func Mode(target uint64) (string, error) {
	for _, mode := range []string{"1M", "10M", "50M", "100M", "250M"} {
		n, _ := contract.Target(mode)
		if n == target {
			return mode, nil
		}
	}
	return "", fmt.Errorf("unsupported target %d; select 1M, 10M, 50M, 100M or 250M", target)
}
