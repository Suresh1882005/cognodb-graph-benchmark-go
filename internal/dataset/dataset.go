// Package dataset handles turning the raw SNAP email-Enron edge list into
// the flat nodes.csv/edges.csv pair every platform adapter loads from, and
// reading those CSVs back for the benchmark run.
package dataset

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type PrepareOptions struct {
	RawTxtPath string
	NodesCSV   string
	EdgesCSV   string
	MaxEdges   int // 0 = no cap
	Seed       int64
}

// Prepare reads the raw SNAP edge list, dedupes, strips self-loops, and
// writes data/nodes.csv + data/edges.csv — the identical input every
// platform loader reads. Mirrors data/prepare_dataset.py from the Python
// version of this repo, kept as a Go program so the whole pipeline is one
// language end to end.
func Prepare(opts PrepareOptions) (nodeCount, edgeCount int, err error) {
	f, err := os.Open(opts.RawTxtPath)
	if err != nil {
		return 0, 0, fmt.Errorf("%s not found — run data/download_dataset.sh first: %w", opts.RawTxtPath, err)
	}
	defer f.Close()

	type edge struct{ src, dst int64 }
	seen := map[edge]struct{}{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		src, err1 := strconv.ParseInt(fields[0], 10, 64)
		dst, err2 := strconv.ParseInt(fields[1], 10, 64)
		if err1 != nil || err2 != nil || src == dst {
			continue // skip self-loops and malformed lines
		}
		seen[edge{src, dst}] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	edges := make([]edge, 0, len(seen))
	for e := range seen {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].src != edges[j].src {
			return edges[i].src < edges[j].src
		}
		return edges[i].dst < edges[j].dst
	})

	if opts.MaxEdges > 0 && len(edges) > opts.MaxEdges {
		rng := rand.New(rand.NewSource(opts.Seed))
		rng.Shuffle(len(edges), func(i, j int) { edges[i], edges[j] = edges[j], edges[i] })
		edges = edges[:opts.MaxEdges]
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].src != edges[j].src {
				return edges[i].src < edges[j].src
			}
			return edges[i].dst < edges[j].dst
		})
	}

	nodeSet := map[int64]struct{}{}
	for _, e := range edges {
		nodeSet[e.src] = struct{}{}
		nodeSet[e.dst] = struct{}{}
	}
	nodes := make([]int64, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })

	if err := os.MkdirAll(filepath.Dir(opts.NodesCSV), 0o755); err != nil {
		return 0, 0, err
	}

	nf, err := os.Create(opts.NodesCSV)
	if err != nil {
		return 0, 0, err
	}
	defer nf.Close()
	nw := bufio.NewWriter(nf)
	nw.WriteString("id\n")
	for _, n := range nodes {
		fmt.Fprintf(nw, "%d\n", n)
	}
	if err := nw.Flush(); err != nil {
		return 0, 0, err
	}

	ef, err := os.Create(opts.EdgesCSV)
	if err != nil {
		return 0, 0, err
	}
	defer ef.Close()
	ew := bufio.NewWriter(ef)
	ew.WriteString("src,dst\n")
	for _, e := range edges {
		fmt.Fprintf(ew, "%d,%d\n", e.src, e.dst)
	}
	if err := ew.Flush(); err != nil {
		return 0, 0, err
	}

	return len(nodes), len(edges), nil
}

// Load reads nodes.csv / edges.csv back for the benchmark run. Every
// platform loader consumes exactly this slice, so ingest/traversal/lookup
// workloads all operate over an identical in-memory dataset.
func Load(nodesCSV, edgesCSV string) (nodeIDs []int64, edges [][2]int64, err error) {
	nf, err := os.Open(nodesCSV)
	if err != nil {
		return nil, nil, fmt.Errorf("%s not found — run: go run ./cmd/prepare-dataset: %w", nodesCSV, err)
	}
	defer nf.Close()
	scanner := bufio.NewScanner(nf)
	scanner.Scan() // header
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, nil, err
		}
		nodeIDs = append(nodeIDs, n)
	}

	ef, err := os.Open(edgesCSV)
	if err != nil {
		return nil, nil, fmt.Errorf("%s not found — run: go run ./cmd/prepare-dataset: %w", edgesCSV, err)
	}
	defer ef.Close()
	scanner = bufio.NewScanner(ef)
	scanner.Scan() // header
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 2 {
			continue
		}
		a, err1 := strconv.ParseInt(parts[0], 10, 64)
		b, err2 := strconv.ParseInt(parts[1], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, nil, fmt.Errorf("bad edge row %q", line)
		}
		edges = append(edges, [2]int64{a, b})
	}

	return nodeIDs, edges, nil
}
