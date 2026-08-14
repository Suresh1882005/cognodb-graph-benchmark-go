// Package report turns results/raw/*_latest.json into results/RESULTS.md
// plus a few SVG bar/line charts. Charts are hand-built SVG (just XML text)
// rather than pulled from a charting library — this repo's only external
// dependencies are the three official database drivers it benchmarks.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/suresh/cognodb-graph-benchmark/internal/config"
	"github.com/suresh/cognodb-graph-benchmark/internal/harness"
)

// Build reads every results/raw/<platform>_latest.json under repoRoot,
// writes results/RESULTS.md, and writes results/charts/*.svg.
func Build(repoRoot string) error {
	rawDir := filepath.Join(repoRoot, "results", "raw")
	chartsDir := filepath.Join(repoRoot, "results", "charts")
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		return err
	}

	results := map[string]harness.Result{}
	for _, key := range config.Keys() {
		p := filepath.Join(rawDir, key+"_latest.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue // not run yet — skip, don't fail the whole report
		}
		var r harness.Result
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parsing %s: %w", p, err)
		}
		results[key] = r
	}

	if len(results) == 0 {
		fmt.Println("No results/raw/*_latest.json files found. Run ./run-benchmark for each platform first.")
		return nil
	}

	// stable order = registry order, only for platforms actually present
	var orderedKeys []string
	for _, key := range config.Keys() {
		if _, ok := results[key]; ok {
			orderedKeys = append(orderedKeys, key)
		}
	}

	var sb strings.Builder
	sb.WriteString("# Results Matrix\n\n")
	fmt.Fprintf(&sb, "_Generated from %d platform result file(s) in `results/raw/`._\n\n", len(results))

	sb.WriteString("## Data loading\n\n")
	sb.WriteString(ingestTable(orderedKeys, results))
	sb.WriteString("\n\n## Traversals (1/2/3-hop)\n\n")
	sb.WriteString(traversalTable(orderedKeys, results))
	sb.WriteString("\n\n## Lookups\n\n")
	sb.WriteString(lookupTable(orderedKeys, results))
	sb.WriteString("\n\n## Aggregation (top-10 by out-degree)\n\n")
	sb.WriteString(aggregationTable(orderedKeys, results))
	sb.WriteString("\n\n## Mixed read/write workload\n\n")
	sb.WriteString(mixedTable(orderedKeys, results))
	sb.WriteString("\n\n## Storage / resource footprint\n\n")
	sb.WriteString(footprintTable(orderedKeys, results))
	sb.WriteString("\n\n## Charts\n\n")
	sb.WriteString("![Ingest throughput](charts/ingest_throughput.svg)\n\n")
	sb.WriteString("![Traversal p95](charts/traversal_p95.svg)\n\n")
	sb.WriteString("![Mixed throughput](charts/mixed_throughput.svg)\n")

	outMD := filepath.Join(repoRoot, "results", "RESULTS.md")
	if err := os.WriteFile(outMD, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", outMD)

	if err := chartIngestThroughput(orderedKeys, results, filepath.Join(chartsDir, "ingest_throughput.svg")); err != nil {
		return err
	}
	if err := chartTraversalP95(orderedKeys, results, filepath.Join(chartsDir, "traversal_p95.svg")); err != nil {
		return err
	}
	if err := chartMixedThroughput(orderedKeys, results, filepath.Join(chartsDir, "mixed_throughput.svg")); err != nil {
		return err
	}
	fmt.Println("wrote charts to", chartsDir)
	return nil
}

// ---- Markdown table builders ----

func mdTable(headers []string, rows [][]string) string {
	var sb strings.Builder
	sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	sb.WriteString("|" + strings.Repeat("---|", len(headers)) + "\n")
	for _, row := range rows {
		sb.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	return sb.String()
}

func f(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

func ptr(p *float64) string {
	if p == nil {
		return "n/a"
	}
	return f(*p)
}

func ingestTable(keys []string, results map[string]harness.Result) string {
	var rows [][]string
	for _, k := range keys {
		r := results[k]
		if r.Ingest == nil {
			rows = append(rows, []string{r.DisplayName, "skipped (--skip-load)", "-", "-", "-"})
			continue
		}
		rows = append(rows, []string{
			r.DisplayName,
			fmt.Sprintf("%.2fs", r.Ingest.WallClockSeconds),
			fmt.Sprintf("%.1f", r.Ingest.NodesPerSec),
			fmt.Sprintf("%.1f", r.Ingest.EdgesPerSec),
			fmt.Sprintf("%d/%d", r.Dataset.Nodes, r.Dataset.Edges),
		})
	}
	return mdTable([]string{"Platform", "Total load time", "Nodes/sec", "Edges/sec", "Nodes/Edges loaded"}, rows)
}

func traversalTable(keys []string, results map[string]harness.Result) string {
	var rows [][]string
	for _, k := range keys {
		r := results[k]
		for _, hopKey := range []string{"1_hop", "2_hop", "3_hop"} {
			t, ok := r.Traversals[hopKey]
			if !ok {
				continue
			}
			rows = append(rows, []string{
				r.DisplayName, strings.Replace(hopKey, "_", "-", 1),
				ptr(t.Summary.P50Ms), ptr(t.Summary.P95Ms),
				f(t.ColdStartMs), strconv.Itoa(t.Summary.N),
			})
		}
	}
	return mdTable([]string{"Platform", "Hop depth", "p50 (ms)", "p95 (ms)", "Cold start (ms)", "n"}, rows)
}

func lookupTable(keys []string, results map[string]harness.Result) string {
	var rows [][]string
	labels := map[string]string{"point_lookup": "Point lookup", "indexed_range_lookup": "Indexed/filtered lookup"}
	for _, k := range keys {
		r := results[k]
		for _, lkKey := range []string{"point_lookup", "indexed_range_lookup"} {
			lk, ok := r.Lookups[lkKey]
			if !ok {
				continue
			}
			rows = append(rows, []string{
				r.DisplayName, labels[lkKey],
				ptr(lk.Summary.P50Ms), ptr(lk.Summary.P95Ms),
				f(lk.ColdStartMs), strconv.Itoa(lk.Summary.N),
			})
		}
	}
	return mdTable([]string{"Platform", "Query", "p50 (ms)", "p95 (ms)", "Cold start (ms)", "n"}, rows)
}

func aggregationTable(keys []string, results map[string]harness.Result) string {
	var rows [][]string
	for _, k := range keys {
		r := results[k]
		agg, ok := r.Aggregation["top10_out_degree"]
		if !ok {
			continue
		}
		rows = append(rows, []string{r.DisplayName, ptr(agg.P50Ms), ptr(agg.P95Ms), strconv.Itoa(agg.N)})
	}
	return mdTable([]string{"Platform", "p50 (ms)", "p95 (ms)", "n"}, rows)
}

func mixedTable(keys []string, results map[string]harness.Result) string {
	var rows [][]string
	for _, k := range keys {
		r := results[k]
		var concKeys []string
		for ck := range r.MixedWorkload {
			concKeys = append(concKeys, ck)
		}
		sort.Slice(concKeys, func(i, j int) bool {
			a, _ := strconv.Atoi(concKeys[i])
			b, _ := strconv.Atoi(concKeys[j])
			return a < b
		})
		for _, ck := range concKeys {
			m := r.MixedWorkload[ck]
			rows = append(rows, []string{
				r.DisplayName, strconv.Itoa(m.Concurrency), fmt.Sprintf("%.2f", m.ThroughputQPS),
				ptr(m.Latency.P50Ms), ptr(m.Latency.P95Ms), strconv.Itoa(m.TotalOps),
			})
		}
	}
	return mdTable([]string{"Platform", "Concurrency", "Throughput (qps)", "p50 (ms)", "p95 (ms)", "Total ops"}, rows)
}

func footprintTable(keys []string, results map[string]harness.Result) string {
	var rows [][]string
	for _, k := range keys {
		r := results[k]
		bytesStr := "n/a"
		if r.Footprint.Observable {
			bytesStr = strconv.FormatInt(r.Footprint.StoredBytes, 10)
		}
		rows = append(rows, []string{r.DisplayName, bytesStr, r.Footprint.Notes})
	}
	return mdTable([]string{"Platform", "Stored bytes (best-effort)", "Notes"}, rows)
}
