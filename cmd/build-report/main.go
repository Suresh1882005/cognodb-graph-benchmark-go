// Command build-report reads every results/raw/<platform>_latest.json
// produced by run-benchmark and generates results/RESULTS.md plus
// results/charts/*.svg.
//
// Usage:
//
//	go run ./cmd/build-report
package main

import (
	"fmt"
	"os"

	"github.com/suresh/cognodb-graph-benchmark/internal/report"
)

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := report.Build(repoRoot); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
