// Package dbclient defines the platform-agnostic interface every graph
// database adapter implements, plus the small result types the harness
// passes around.
//
// Design principle (same as the Python version this replaces): adapters
// only know how to DO an operation in their native dialect (Cypher, AQL,
// ...). They never time themselves. internal/harness/runner.go wraps every
// call in time.Now()/time.Since() itself, so every platform is timed with
// the exact same stopwatch code, iteration counts, and percentile math.
// That symmetry is the whole point of the fairness story — see README.
//
// Data model used by every adapter (identical across all five platforms):
//
//	(:Person {id: INT})-[:EMAILED]->(:Person {id: INT})
package dbclient

import "context"

// LoadResult is returned by BulkLoad. Adapters measure their own wall clock
// time for this one operation, since batching strategy is itself part of
// what's being compared — see README > Data loading method per platform.
type LoadResult struct {
	NodesLoaded      int
	EdgesLoaded      int
	WallClockSeconds float64
}

func (r LoadResult) NodesPerSec() float64 {
	if r.WallClockSeconds <= 0 {
		return 0
	}
	return float64(r.NodesLoaded) / r.WallClockSeconds
}

func (r LoadResult) EdgesPerSec() float64 {
	if r.WallClockSeconds <= 0 {
		return 0
	}
	return float64(r.EdgesLoaded) / r.WallClockSeconds
}

// FootprintResult is a best-effort storage/memory report. StoredBytes is
// nil (via the Observable flag) when a platform's free tier exposes
// nothing — reported honestly as "not observable" rather than guessed.
type FootprintResult struct {
	Observable  bool
	StoredBytes int64
	Notes       string
}

// GraphClient is implemented once per wire protocol/dialect:
//   - CypherBoltClient  -> CognoDB, Neo4j AuraDB, Memgraph (all Bolt+Cypher)
//   - FalkorDBClient    -> FalkorDB (Redis protocol, Cypher subset)
//   - ArangoDBClient    -> ArangoDB (AQL)
type GraphClient interface {
	// Key is the short machine-readable platform key, matches internal/config.
	Key() string
	// DisplayName is the human-readable name used in reports.
	DisplayName() string
	// IndexedProperties lists what's indexed, for the §5.2 "state which
	// properties are indexed" requirement.
	IndexedProperties() []string

	Connect(ctx context.Context) error
	Close(ctx context.Context) error

	// ClearDatabase wipes all nodes/edges so repeated runs start from a
	// known empty state.
	ClearDatabase(ctx context.Context) error
	// CreateIndexes creates whatever index/constraint this platform uses
	// for Person.id.
	CreateIndexes(ctx context.Context) error
	// BulkLoad loads nodes then edges in batches and measures its own
	// wall-clock time (see LoadResult doc comment above).
	BulkLoad(ctx context.Context, nodeIDs []int64, edges [][2]int64, batchSize int) (LoadResult, error)

	// ---- read workloads: the harness times these calls, adapters just execute ----

	// Traversal returns the count of distinct nodes reachable in exactly
	// `hops` hops outbound via EMAILED, capped at 10,000 for cross-platform
	// comparability on high out-degree nodes (see docs/QUERIES.md).
	Traversal(ctx context.Context, startID int64, hops int) (int64, error)
	// PointLookup is a single exact-match fetch of one Person by id.
	PointLookup(ctx context.Context, nodeID int64) (bool, error)
	// IndexedRangeLookup counts Persons with lo <= id < hi using the same
	// index as PointLookup — the "indexed/filtered lookup" metric.
	IndexedRangeLookup(ctx context.Context, lo, hi int64) (int64, error)
	// AggregationTopOutDegree is the group-by style aggregation: top-N
	// Persons by outbound EMAILED count.
	AggregationTopOutDegree(ctx context.Context, limit int) ([]OutDegreeRow, error)

	// ---- mixed workload primitives ----

	// MixedReadOp is one lightweight read used inside the concurrent mixed workload.
	MixedReadOp(ctx context.Context, nodeID int64) error
	// MixedWriteOp is one lightweight, non-growing write (property update)
	// used inside the concurrent mixed workload — must not change node/edge
	// counts, so repeated runs don't drift the dataset size.
	MixedWriteOp(ctx context.Context, nodeID int64) error

	// StorageFootprint is a best-effort stored-size/memory report.
	StorageFootprint(ctx context.Context) (FootprintResult, error)
}

// OutDegreeRow is one row of the top-N-by-out-degree aggregation result.
type OutDegreeRow struct {
	PersonID int64
	Sent     int64
}
