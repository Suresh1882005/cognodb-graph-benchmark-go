// Command prepare-dataset turns the raw SNAP email-Enron edge list into
// data/nodes.csv + data/edges.csv, the identical input every platform
// adapter loads from.
//
// Usage:
//
//	go run ./cmd/prepare-dataset
//	go run ./cmd/prepare-dataset --max-edges 300000
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/suresh/cognodb-graph-benchmark/internal/dataset"
)

func main() {
	maxEdges := flag.Int("max-edges", 0, "optionally cap the number of edges (random sample, seeded)")
	seed := flag.Int64("seed", 42, "random seed used only if --max-edges triggers sampling")
	flag.Parse()

	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	nodes, edges, err := dataset.Prepare(dataset.PrepareOptions{
		RawTxtPath: filepath.Join(repoRoot, "data", "email-Enron.txt"),
		NodesCSV:   filepath.Join(repoRoot, "data", "nodes.csv"),
		EdgesCSV:   filepath.Join(repoRoot, "data", "edges.csv"),
		MaxEdges:   *maxEdges,
		Seed:       *seed,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote data/nodes.csv (%d nodes)\n", nodes)
	fmt.Printf("Wrote data/edges.csv (%d edges)\n", edges)
	fmt.Println("These two files are what every platform loader reads — identical input for every platform.")
}
