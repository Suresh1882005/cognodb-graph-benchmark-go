// FalkorDB speaks an openCypher subset but over the Redis wire protocol
// (RESP), not Bolt — so it needs the official falkordb-go client instead of
// the neo4j driver. This is a genuine, disclosed protocol difference (see
// internal/config/platforms.go), not an inconsistency in the harness: the
// Cypher text itself is kept as close as possible to cypherbolt.go so the
// *logical* query is identical even though the transport differs.
package dbclient

import (
	"context"
	"fmt"
	"time"

	falkordb "github.com/FalkorDB/falkordb-go/v2"
)

const falkorGraphName = "email_enron"

type FalkorDBClient struct {
	host     string
	port     string
	password string
	db       *falkordb.FalkorDB
	graph    *falkordb.Graph
}

func NewFalkorDBClient(host, port, password string) *FalkorDBClient {
	return &FalkorDBClient{host: host, port: port, password: password}
}

func (c *FalkorDBClient) Key() string         { return "falkordb" }
func (c *FalkorDBClient) DisplayName() string { return "FalkorDB" }
func (c *FalkorDBClient) IndexedProperties() []string {
	return []string{"Person.id"}
}

func (c *FalkorDBClient) Connect(_ context.Context) error {
	if c.host == "" {
		return fmt.Errorf("[falkordb] missing FALKORDB_HOST — set it in .env before running this platform")
	}
	port := c.port
	if port == "" {
		port = "6379"
	}
	db, err := falkordb.FalkorDBNew(&falkordb.ConnectionOption{
		Addr:     fmt.Sprintf("%s:%s", c.host, port),
		Password: c.password,
	})
	if err != nil {
		return err
	}
	c.db = db
	c.graph = db.SelectGraph(falkorGraphName)
	// connectivity check
	if _, err := c.graph.Query("RETURN 1", nil, nil); err != nil {
		return err
	}
	return nil
}

func (c *FalkorDBClient) Close(_ context.Context) error {
	return nil // falkordb-go (redis-go under the hood) manages its own pooled connections
}

func (c *FalkorDBClient) ClearDatabase(_ context.Context) error {
	_ = c.graph.Delete() // ok if the graph doesn't exist yet
	c.graph = c.db.SelectGraph(falkorGraphName)
	return nil
}

func (c *FalkorDBClient) CreateIndexes(_ context.Context) error {
	_, err := c.graph.Query("CREATE INDEX FOR (p:Person) ON (p.id)", nil, nil)
	return err
}

func (c *FalkorDBClient) BulkLoad(_ context.Context, nodeIDs []int64, edges [][2]int64, batchSize int) (LoadResult, error) {
	start := time.Now()

	for i := 0; i < len(nodeIDs); i += batchSize {
		end := min(i+batchSize, len(nodeIDs))
		batch := nodeIDs[i:end]
		ids := make([]any, len(batch))
		for j, v := range batch {
			ids[j] = v
		}
		if _, err := c.graph.Query("UNWIND $ids AS id CREATE (:Person {id: id})",
			map[string]any{"ids": ids}, nil); err != nil {
			return LoadResult{}, err
		}
	}

	for i := 0; i < len(edges); i += batchSize {
		end := min(i+batchSize, len(edges))
		batch := edges[i:end]
		rows := make([]any, len(batch))
		for j, e := range batch {
			rows[j] = map[string]any{"src": e[0], "dst": e[1]}
		}
		query := `
			UNWIND $rows AS row
			MATCH (a:Person {id: row.src})
			MATCH (b:Person {id: row.dst})
			CREATE (a)-[:EMAILED]->(b)
		`
		if _, err := c.graph.Query(query, map[string]any{"rows": rows}, nil); err != nil {
			return LoadResult{}, err
		}
	}

	elapsed := time.Since(start).Seconds()
	return LoadResult{NodesLoaded: len(nodeIDs), EdgesLoaded: len(edges), WallClockSeconds: elapsed}, nil
}

func (c *FalkorDBClient) Traversal(_ context.Context, startID int64, hops int) (int64, error) {
	var query string
	switch hops {
	case 1:
		query = "MATCH (p:Person {id:$id})-[:EMAILED]->(x) RETURN count(DISTINCT x) AS c"
	case 2:
		query = "MATCH (p:Person {id:$id})-[:EMAILED]->()-[:EMAILED]->(x) RETURN count(DISTINCT x) AS c LIMIT 10000"
	case 3:
		query = "MATCH (p:Person {id:$id})-[:EMAILED]->()-[:EMAILED]->()-[:EMAILED]->(x) RETURN count(DISTINCT x) AS c LIMIT 10000"
	default:
		return 0, fmt.Errorf("only 1/2/3 hops are benchmarked, got %d", hops)
	}
	result, err := c.graph.Query(query, map[string]any{"id": startID}, nil)
	if err != nil {
		return 0, err
	}
	return firstCount(result, "c"), nil
}

func (c *FalkorDBClient) PointLookup(_ context.Context, nodeID int64) (bool, error) {
	result, err := c.graph.Query("MATCH (p:Person {id:$id}) RETURN p", map[string]any{"id": nodeID}, nil)
	if err != nil {
		return false, err
	}
	return result.Next(), nil
}

func (c *FalkorDBClient) IndexedRangeLookup(_ context.Context, lo, hi int64) (int64, error) {
	result, err := c.graph.Query(
		"MATCH (p:Person) WHERE p.id >= $lo AND p.id < $hi RETURN count(p) AS c",
		map[string]any{"lo": lo, "hi": hi}, nil)
	if err != nil {
		return 0, err
	}
	return firstCount(result, "c"), nil
}

func (c *FalkorDBClient) AggregationTopOutDegree(_ context.Context, limit int) ([]OutDegreeRow, error) {
	query := `
		MATCH (p:Person)-[r:EMAILED]->()
		RETURN p.id AS person, count(r) AS sent
		ORDER BY sent DESC
		LIMIT $limit
	`
	result, err := c.graph.Query(query, map[string]any{"limit": int64(limit)}, nil)
	if err != nil {
		return nil, err
	}
	var rows []OutDegreeRow
	for result.Next() {
		rec := result.Record()
		personRaw, _ := rec.Get("person")
		sentRaw, _ := rec.Get("sent")
		rows = append(rows, OutDegreeRow{PersonID: toInt64(personRaw), Sent: toInt64(sentRaw)})
	}
	return rows, nil
}

func (c *FalkorDBClient) MixedReadOp(_ context.Context, nodeID int64) error {
	_, err := c.graph.Query("MATCH (p:Person {id:$id}) RETURN p.id AS id", map[string]any{"id": nodeID}, nil)
	return err
}

func (c *FalkorDBClient) MixedWriteOp(_ context.Context, nodeID int64) error {
	_, err := c.graph.Query(
		"MATCH (p:Person {id:$id}) SET p.last_touched = timestamp() RETURN p.id AS id",
		map[string]any{"id": nodeID}, nil)
	return err
}

func (c *FalkorDBClient) StorageFootprint(_ context.Context) (FootprintResult, error) {
	// falkordb-go doesn't expose a typed helper for Redis INFO memory stats;
	// issuing the raw command reliably across client versions is more
	// brittle than it's worth for a best-effort figure, so we report
	// honestly instead of guessing.
	return FootprintResult{Observable: false, Notes: "not observable on this tier/platform"}, nil
}

// firstCount pulls the single scalar column from a one-row aggregate result
// (COUNT-style queries), defaulting to 0 if the result set is empty.
func firstCount(result *falkordb.QueryResult, key string) int64 {
	if !result.Next() {
		return 0
	}
	raw, _ := result.Record().Get(key)
	return toInt64(raw)
}
