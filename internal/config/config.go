// Package config centralizes everything read from .env and the static
// platform registry, so there is exactly one place that parses environment
// variables — this is what keeps the harness "one binary, many platforms"
// instead of five diverging programs.
//
// Deliberately dependency-free: a ~30-line .env parser instead of pulling
// in a third-party dotenv package, since this repo's only external
// dependencies are the three official database drivers it's benchmarking.
package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Env wraps process environment variables, pre-loaded from .env if present.
type Env struct {
	vars map[string]string
}

// LoadEnv reads .env (if it exists) into memory, then overlays real
// process environment variables on top (so real env vars always win over
// .env — useful for CI or one-off overrides without editing the file).
func LoadEnv(dotEnvPath string) (*Env, error) {
	e := &Env{vars: map[string]string{}}

	if f, err := os.Open(dotEnvPath); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			e.vars[key] = val
		}
	}
	// os.Environ() overrides .env, matching python-dotenv's default behavior
	// of not clobbering variables the shell already set.
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			e.vars[parts[0]] = parts[1]
		}
	}
	return e, nil
}

func (e *Env) Get(key, fallback string) string {
	if v, ok := e.vars[key]; ok && v != "" {
		return v
	}
	return fallback
}

func (e *Env) GetInt(key string, fallback int) int {
	if v, ok := e.vars[key]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func (e *Env) GetFloat(key string, fallback float64) float64 {
	if v, ok := e.vars[key]; ok && v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return fallback
}

// PlatformVars pulls every <prefix>_* variable into a plain map with the
// prefix stripped and upper-cased suffix keys, e.g. prefix "COGNODB" ->
// {"URI": ..., "USER": ..., "PASSWORD": ...}.
func (e *Env) PlatformVars(prefix string) map[string]string {
	out := map[string]string{}
	want := prefix + "_"
	for k, v := range e.vars {
		if strings.HasPrefix(k, want) {
			out[strings.ToUpper(strings.TrimPrefix(k, want))] = v
		}
	}
	return out
}

// BenchConfig holds the shared benchmark parameters (iterations, warmup,
// concurrency sweep, etc.) — identical meaning to the Python version's
// BenchConfig, read from the same env var names so .env is unchanged.
type BenchConfig struct {
	Iterations          int
	Warmup              int
	ConcurrencyLevels   []int
	MixedDurationSec    int
	ReadWriteRatio      float64
	TraversalSampleSize int
	RandomSeed          int64
}

func LoadBenchConfig(e *Env) BenchConfig {
	levels := []int{1, 10, 40}
	if raw := e.Get("BENCH_CONCURRENCY_LEVELS", ""); raw != "" {
		levels = nil
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				levels = append(levels, n)
			}
		}
	}
	return BenchConfig{
		Iterations:          e.GetInt("BENCH_ITERATIONS", 100),
		Warmup:              e.GetInt("BENCH_WARMUP", 20),
		ConcurrencyLevels:   levels,
		MixedDurationSec:    e.GetInt("BENCH_MIXED_DURATION_SEC", 30),
		ReadWriteRatio:      e.GetFloat("BENCH_READ_WRITE_RATIO", 0.8),
		TraversalSampleSize: e.GetInt("BENCH_TRAVERSAL_SAMPLE_SIZE", 50),
		RandomSeed:          int64(e.GetInt("BENCH_RANDOM_SEED", 42)),
	}
}
