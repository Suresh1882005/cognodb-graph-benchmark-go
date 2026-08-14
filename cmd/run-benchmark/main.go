// Command run-benchmark is the single entry point that loads data into ONE
// platform, warms it up, measures every metric required by assignment
// §5.2, and writes a JSON results file.
//
// Usage:
//
//	go run ./cmd/run-benchmark --platform cognodb --fresh
//	go run ./cmd/run-benchmark --platform neo4j_aura --fresh
//	go run ./cmd/run-benchmark --platform memgraph --fresh
//	go run ./cmd/run-benchmark --platform falkordb --fresh
//	go run ./cmd/run-benchmark --platform arangodb --fresh
//
// Run once per platform (see README > How to reproduce). Every platform
// goes through the exact same code path in internal/harness/runner.go — the
// only thing that varies is which GraphClient implementation is plugged in.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/suresh/cognodb-graph-benchmark/internal/config"
	"github.com/suresh/cognodb-graph-benchmark/internal/harness"
)

func main() {
	platform := flag.String("platform", "", "platform key, e.g. cognodb, neo4j_aura, memgraph, falkordb, arangodb")
	fresh := flag.Bool("fresh", false, "wipe the database before loading")
	skipLoad := flag.Bool("skip-load", false, "assume data is already loaded; skip straight to workloads")
	batchSize := flag.Int("batch-size", 1000, "rows per batch during bulk load")
	flag.Parse()

	if *platform == "" {
		fmt.Fprintln(os.Stderr, "error: --platform is required (one of:", config.Keys(), ")")
		os.Exit(1)
	}
	if _, ok := config.ByKey(*platform); !ok {
		fmt.Fprintf(os.Stderr, "error: unknown platform %q (known: %v)\n", *platform, config.Keys())
		os.Exit(1)
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	env, err := config.LoadEnv(filepath.Join(repoRoot, ".env"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error loading .env:", err)
		os.Exit(1)
	}

	err = harness.RunBenchmark(context.Background(), harness.RunOptions{
		PlatformKey: *platform,
		Fresh:       *fresh,
		SkipLoad:    *skipLoad,
		BatchSize:   *batchSize,
		RepoRoot:    repoRoot,
		Env:         env,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
