package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/suresh/cognodb-graph-benchmark/internal/config"
	"github.com/suresh/cognodb-graph-benchmark/internal/dataset"
	"github.com/suresh/cognodb-graph-benchmark/internal/dbclient"
)

// ---- result JSON schema (mirrors the Python version's results/raw/*.json) ----

type Result struct {
	Platform          string                    `json:"platform"`
	DisplayName       string                    `json:"display_name"`
	AdvertisedSpecs   map[string]string         `json:"advertised_specs"`
	IndexedProperties []string                  `json:"indexed_properties"`
	GeneratedAtUTC    string                    `json:"generated_at_utc"`
	Dataset           DatasetInfo               `json:"dataset"`
	BenchConfig       BenchConfigInfo           `json:"bench_config"`
	Ingest            *IngestInfo               `json:"ingest"`
	Traversals        map[string]HopResult      `json:"traversals"`
	Lookups           map[string]LookupResult   `json:"lookups"`
	Aggregation       map[string]LatencySummary `json:"aggregation"`
	MixedWorkload     map[string]MixedResult    `json:"mixed_workload"`
	Footprint         FootprintInfo             `json:"footprint"`
	Caveats           []string                  `json:"caveats"`
}

type DatasetInfo struct {
	Nodes  int    `json:"nodes"`
	Edges  int    `json:"edges"`
	Source string `json:"source"`
}

type BenchConfigInfo struct {
	Iterations          int     `json:"iterations"`
	Warmup              int     `json:"warmup"`
	ConcurrencyLevels   []int   `json:"concurrency_levels"`
	MixedDurationSec    int     `json:"mixed_duration_sec"`
	ReadWriteRatio      float64 `json:"read_write_ratio"`
	TraversalSampleSize int     `json:"traversal_sample_size"`
}

type IngestInfo struct {
	NodesLoaded      int     `json:"nodes_loaded"`
	EdgesLoaded      int     `json:"edges_loaded"`
	WallClockSeconds float64 `json:"wall_clock_seconds"`
	NodesPerSec      float64 `json:"nodes_per_sec"`
	EdgesPerSec      float64 `json:"edges_per_sec"`
}

type HopResult struct {
	Summary     LatencySummary `json:"summary"`
	ColdStartMs float64        `json:"cold_start_ms"`
}

type LookupResult struct {
	Summary     LatencySummary `json:"summary"`
	ColdStartMs float64        `json:"cold_start_ms"`
}

type MixedResult struct {
	Concurrency    int            `json:"concurrency"`
	DurationSec    int            `json:"duration_sec"`
	TotalOps       int            `json:"total_ops"`
	ThroughputQPS  float64        `json:"throughput_qps"`
	Latency        LatencySummary `json:"latency"`
	ReadWriteRatio float64        `json:"read_write_ratio"`
}

type FootprintInfo struct {
	Observable  bool   `json:"observable"`
	StoredBytes int64  `json:"stored_bytes"`
	Notes       string `json:"notes"`
}

// ---- run options ----

type RunOptions struct {
	PlatformKey string
	Fresh       bool
	SkipLoad    bool
	BatchSize   int
	RepoRoot    string // directory containing data/ and results/
	Env         *config.Env
}

// RunBenchmark is the single entry point cmd/run-benchmark calls: connect,
// (optionally) wipe + load, warm up, measure every §5.2 metric, write JSON.
// Every platform goes through this exact same code path — same iteration
// counts, same percentile math, same mixed-workload driver — so the only
// thing that varies between runs is which GraphClient is plugged in.
func RunBenchmark(ctx context.Context, opts RunOptions) error {
	cfg := config.LoadBenchConfig(opts.Env)
	spec, ok := config.ByKey(opts.PlatformKey)
	if !ok {
		return fmt.Errorf("unknown platform %q", opts.PlatformKey)
	}

	fmt.Printf("=== %s ===\n", spec.DisplayName)
	client, err := dbclient.Build(opts.PlatformKey, opts.Env)
	if err != nil {
		return err
	}
	if err := client.Connect(ctx); err != nil {
		return err
	}
	fmt.Println("connected.")

	nodesCSV := filepath.Join(opts.RepoRoot, "data", "nodes.csv")
	edgesCSV := filepath.Join(opts.RepoRoot, "data", "edges.csv")
	nodeIDs, edges, err := dataset.Load(nodesCSV, edgesCSV)
	if err != nil {
		return err
	}
	fmt.Printf("dataset: %d nodes, %d edges\n", len(nodeIDs), len(edges))

	rng := rand.New(rand.NewSource(cfg.RandomSeed))

	result := Result{
		Platform:    opts.PlatformKey,
		DisplayName: spec.DisplayName,
		AdvertisedSpecs: map[string]string{
			"vcpu": spec.AdvertisedVCPU, "ram": spec.AdvertisedRAM,
			"disk": spec.AdvertisedDisk, "tier_name": spec.TierName, "source": spec.SpecSource,
		},
		IndexedProperties: client.IndexedProperties(),
		GeneratedAtUTC:    time.Now().UTC().Format(time.RFC3339),
		Dataset:           DatasetInfo{Nodes: len(nodeIDs), Edges: len(edges), Source: "SNAP email-Enron"},
		BenchConfig: BenchConfigInfo{
			Iterations: cfg.Iterations, Warmup: cfg.Warmup, ConcurrencyLevels: cfg.ConcurrencyLevels,
			MixedDurationSec: cfg.MixedDurationSec, ReadWriteRatio: cfg.ReadWriteRatio,
			TraversalSampleSize: cfg.TraversalSampleSize,
		},
		Traversals:    map[string]HopResult{},
		Lookups:       map[string]LookupResult{},
		Aggregation:   map[string]LatencySummary{},
		MixedWorkload: map[string]MixedResult{},
		Caveats:       []string{},
	}

	if !opts.SkipLoad {
		if opts.Fresh {
			fmt.Println("clearing existing data...")
			if err := client.ClearDatabase(ctx); err != nil {
				return fmt.Errorf("clear database: %w", err)
			}
		}
		fmt.Println("creating indexes...")
		if err := client.CreateIndexes(ctx); err != nil {
			return fmt.Errorf("create indexes: %w", err)
		}
		fmt.Println("loading data (this can take a while on free-tier instances)...")
		edgePairs := make([][2]int64, len(edges))
		copy(edgePairs, edges)
		lr, err := client.BulkLoad(ctx, nodeIDs, edgePairs, opts.BatchSize)
		if err != nil {
			return fmt.Errorf("bulk load: %w", err)
		}
		fmt.Printf("  loaded in %.2fs (%.1f nodes/s, %.1f edges/s)\n", lr.WallClockSeconds, lr.NodesPerSec(), lr.EdgesPerSec())
		result.Ingest = &IngestInfo{
			NodesLoaded: lr.NodesLoaded, EdgesLoaded: lr.EdgesLoaded, WallClockSeconds: round3(lr.WallClockSeconds),
			NodesPerSec: round2(lr.NodesPerSec()), EdgesPerSec: round2(lr.EdgesPerSec()),
		}
	} else {
		fmt.Println("skipping load (--skip-load passed).")
	}

	// ---- traversals ----
	fmt.Println("running traversal workloads (1/2/3-hop)...")
	for _, hops := range []int{1, 2, 3} {
		summary, coldMs, err := runTraversalWorkload(ctx, client, nodeIDs, hops, cfg.TraversalSampleSize, cfg.Warmup, rng)
		if err != nil {
			return fmt.Errorf("%d-hop traversal: %w", hops, err)
		}
		result.Traversals[fmt.Sprintf("%d_hop", hops)] = HopResult{Summary: summary, ColdStartMs: round3(coldMs)}
		fmt.Printf("  %d-hop: p50=%.2fms  p95=%.2fms  cold=%.2fms\n", hops, deref(summary.P50Ms), deref(summary.P95Ms), coldMs)
	}

	// ---- lookups ----
	fmt.Println("running lookup workloads...")
	pointSummary, pointCold, err := runReadWorkload(nodeIDs, cfg.Iterations, cfg.Warmup, rng, func(id int64) error {
		_, err := client.PointLookup(ctx, id)
		return err
	})
	if err != nil {
		return fmt.Errorf("point lookup: %w", err)
	}
	result.Lookups["point_lookup"] = LookupResult{Summary: pointSummary, ColdStartMs: round3(pointCold)}
	fmt.Printf("  point lookup: p50=%.2fms  p95=%.2fms\n", deref(pointSummary.P50Ms), deref(pointSummary.P95Ms))

	idxSummary, idxCold, err := runReadWorkload(nodeIDs, cfg.Iterations, cfg.Warmup, rng, func(id int64) error {
		_, err := client.IndexedRangeLookup(ctx, id, id+1000)
		return err
	})
	if err != nil {
		return fmt.Errorf("indexed range lookup: %w", err)
	}
	result.Lookups["indexed_range_lookup"] = LookupResult{Summary: idxSummary, ColdStartMs: round3(idxCold)}
	fmt.Printf("  indexed range lookup: p50=%.2fms  p95=%.2fms\n", deref(idxSummary.P50Ms), deref(idxSummary.P95Ms))

	// ---- aggregation ----
	fmt.Println("running aggregation workload...")
	for i := 0; i < cfg.Warmup; i++ {
		if _, err := client.AggregationTopOutDegree(ctx, 10); err != nil {
			return fmt.Errorf("aggregation warmup: %w", err)
		}
	}
	var aggLatencies []time.Duration
	for i := 0; i < cfg.Iterations; i++ {
		start := time.Now()
		if _, err := client.AggregationTopOutDegree(ctx, 10); err != nil {
			return fmt.Errorf("aggregation: %w", err)
		}
		aggLatencies = append(aggLatencies, time.Since(start))
	}
	aggSummary := SummarizeLatencies(aggLatencies)
	result.Aggregation["top10_out_degree"] = aggSummary
	fmt.Printf("  aggregation: p50=%.2fms  p95=%.2fms\n", deref(aggSummary.P50Ms), deref(aggSummary.P95Ms))

	fmt.Println("closing single-connection client before mixed workload sweep...")
	if err := client.Close(ctx); err != nil {
		return err
	}

	// ---- mixed workload sweep ----
	fmt.Printf("running mixed workload sweep (concurrency=%v, %d%% reads, %ds per step)...\n",
		cfg.ConcurrencyLevels, int(cfg.ReadWriteRatio*100), cfg.MixedDurationSec)
	mixed, err := runMixedWorkloadSweep(ctx, opts.PlatformKey, opts.Env, nodeIDs, cfg.ConcurrencyLevels,
		cfg.MixedDurationSec, cfg.ReadWriteRatio, cfg.RandomSeed)
	if err != nil {
		return fmt.Errorf("mixed workload sweep: %w", err)
	}
	result.MixedWorkload = mixed

	// ---- footprint (fresh connection) ----
	client2, err := dbclient.Build(opts.PlatformKey, opts.Env)
	if err != nil {
		return err
	}
	if err := client2.Connect(ctx); err != nil {
		return err
	}
	fp, err := client2.StorageFootprint(ctx)
	if err != nil {
		return err
	}
	result.Footprint = FootprintInfo{Observable: fp.Observable, StoredBytes: fp.StoredBytes, Notes: fp.Notes}
	_ = client2.Close(ctx)

	// ---- write results ----
	outDir := filepath.Join(opts.RepoRoot, "results", "raw")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	outPath := filepath.Join(outDir, fmt.Sprintf("%s_%s.json", opts.PlatformKey, ts))
	latestPath := filepath.Join(outDir, fmt.Sprintf("%s_latest.json", opts.PlatformKey))

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(latestPath, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("\nwrote %s\n", outPath)
	fmt.Printf("wrote %s  (build-report reads the *_latest.json files)\n", latestPath)
	return nil
}

// ---- read workload helpers ----

func runReadWorkload(sampleIDs []int64, iterations, warmup int, rng *rand.Rand, fn func(id int64) error) (LatencySummary, float64, error) {
	if len(sampleIDs) == 0 {
		return LatencySummary{}, 0, fmt.Errorf("no sample ids to read from")
	}
	firstID := sampleIDs[rng.Intn(len(sampleIDs))]
	start := time.Now()
	if err := fn(firstID); err != nil {
		return LatencySummary{}, 0, err
	}
	coldMs := float64(time.Since(start).Microseconds()) / 1000.0

	for i := 0; i < warmup; i++ {
		if err := fn(sampleIDs[rng.Intn(len(sampleIDs))]); err != nil {
			return LatencySummary{}, 0, err
		}
	}

	latencies := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		id := sampleIDs[rng.Intn(len(sampleIDs))]
		start := time.Now()
		if err := fn(id); err != nil {
			return LatencySummary{}, 0, err
		}
		latencies = append(latencies, time.Since(start))
	}
	return SummarizeLatencies(latencies), coldMs, nil
}

func runTraversalWorkload(ctx context.Context, client dbclient.GraphClient, nodeIDs []int64, hops, sampleSize, warmup int, rng *rand.Rand) (LatencySummary, float64, error) {
	starts := make([]int64, sampleSize)
	for i := range starts {
		starts[i] = nodeIDs[rng.Intn(len(nodeIDs))]
	}

	start := time.Now()
	if _, err := client.Traversal(ctx, starts[0], hops); err != nil {
		return LatencySummary{}, 0, err
	}
	coldMs := float64(time.Since(start).Microseconds()) / 1000.0

	warmupN := warmup
	if warmupN > len(starts) {
		warmupN = len(starts)
	}
	for i := 0; i < warmupN; i++ {
		if _, err := client.Traversal(ctx, starts[i], hops); err != nil {
			return LatencySummary{}, 0, err
		}
	}

	latencies := make([]time.Duration, 0, len(starts))
	for _, sid := range starts {
		start := time.Now()
		if _, err := client.Traversal(ctx, sid, hops); err != nil {
			return LatencySummary{}, 0, err
		}
		latencies = append(latencies, time.Since(start))
	}
	return SummarizeLatencies(latencies), coldMs, nil
}

// ---- mixed workload sweep (goroutines, not threads — this is where the Go
// version diverges most visibly in style from the Python ThreadPoolExecutor
// version, while measuring the exact same thing) ----

type mixedWorkerResult struct {
	ops       int
	latencies []time.Duration
}

func mixedWorker(ctx context.Context, platformKey string, env *config.Env, nodeIDs []int64, seed int64, readWriteRatio float64, duration time.Duration) (mixedWorkerResult, error) {
	client, err := dbclient.Build(platformKey, env)
	if err != nil {
		return mixedWorkerResult{}, err
	}
	if err := client.Connect(ctx); err != nil {
		return mixedWorkerResult{}, err
	}
	defer client.Close(ctx)

	rng := rand.New(rand.NewSource(seed))
	deadline := time.Now().Add(duration)
	var result mixedWorkerResult

	for time.Now().Before(deadline) {
		id := nodeIDs[rng.Intn(len(nodeIDs))]
		isRead := rng.Float64() < readWriteRatio
		start := time.Now()
		var opErr error
		if isRead {
			opErr = client.MixedReadOp(ctx, id)
		} else {
			opErr = client.MixedWriteOp(ctx, id)
		}
		if opErr != nil {
			return result, opErr
		}
		result.latencies = append(result.latencies, time.Since(start))
		result.ops++
	}
	return result, nil
}

func runMixedWorkloadSweep(ctx context.Context, platformKey string, env *config.Env, nodeIDs []int64,
	concurrencyLevels []int, durationSec int, readWriteRatio float64, seed int64) (map[string]MixedResult, error) {

	out := map[string]MixedResult{}
	duration := time.Duration(durationSec) * time.Second

	for _, concurrency := range concurrencyLevels {
		type outcome struct {
			r   mixedWorkerResult
			err error
		}
		ch := make(chan outcome, concurrency)
		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(workerSeed int64) {
				defer wg.Done()
				r, err := mixedWorker(ctx, platformKey, env, nodeIDs, workerSeed, readWriteRatio, duration)
				ch <- outcome{r, err}
			}(seed + int64(i))
		}
		wg.Wait()
		close(ch)

		totalOps := 0
		var allLatencies []time.Duration
		for o := range ch {
			if o.err != nil {
				return nil, o.err
			}
			totalOps += o.r.ops
			allLatencies = append(allLatencies, o.r.latencies...)
		}

		summary := SummarizeLatencies(allLatencies)
		throughput := round2(float64(totalOps) / float64(durationSec))
		out[strconv.Itoa(concurrency)] = MixedResult{
			Concurrency: concurrency, DurationSec: durationSec, TotalOps: totalOps,
			ThroughputQPS: throughput, Latency: summary, ReadWriteRatio: readWriteRatio,
		}
		fmt.Printf("    concurrency=%3d  ->  %8.2f qps (p50=%.2fms, p95=%.2fms)\n",
			concurrency, throughput, deref(summary.P50Ms), deref(summary.P95Ms))
	}
	return out, nil
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
