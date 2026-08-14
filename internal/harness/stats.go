// Package harness contains the benchmark orchestration: percentile math,
// the read-workload runner, the goroutine-based mixed-workload sweep, and
// the top-level RunBenchmark entry point cmd/run-benchmark calls into.
package harness

import (
	"sort"
	"time"
)

// LatencySummary mirrors the Python version's summarize_latencies_ms output
// shape, so results/raw/*.json stays readable the same way regardless of
// which language produced it.
type LatencySummary struct {
	N      int      `json:"n"`
	P50Ms  *float64 `json:"p50_ms"`
	P95Ms  *float64 `json:"p95_ms"`
	P99Ms  *float64 `json:"p99_ms"`
	MinMs  *float64 `json:"min_ms"`
	MaxMs  *float64 `json:"max_ms"`
	MeanMs *float64 `json:"mean_ms"`
}

// percentile computes the nearest-rank-interpolated percentile (pct in
// [0,100]) over an already-sorted slice — same linear-interpolation method
// as the Python version's stats.percentile, for numerically comparable output.
func percentile(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	k := (pct / 100) * float64(len(sorted)-1)
	f := int(k)
	c := f + 1
	if c >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := k - float64(f)
	return sorted[f]*(1-frac) + sorted[c]*frac
}

// SummarizeLatencies takes raw per-call latencies (time.Duration) and
// returns a percentile summary in milliseconds.
func SummarizeLatencies(latencies []time.Duration) LatencySummary {
	if len(latencies) == 0 {
		return LatencySummary{N: 0}
	}
	ms := make([]float64, len(latencies))
	sum := 0.0
	for i, d := range latencies {
		v := float64(d.Microseconds()) / 1000.0
		ms[i] = v
		sum += v
	}
	sort.Float64s(ms)

	p50 := round3(percentile(ms, 50))
	p95 := round3(percentile(ms, 95))
	p99 := round3(percentile(ms, 99))
	minV := round3(ms[0])
	maxV := round3(ms[len(ms)-1])
	mean := round3(sum / float64(len(ms)))

	return LatencySummary{
		N: len(ms), P50Ms: &p50, P95Ms: &p95, P99Ms: &p99, MinMs: &minV, MaxMs: &maxV, MeanMs: &mean,
	}
}

func round3(v float64) float64 {
	return float64(int64(v*1000+0.5)) / 1000
}
