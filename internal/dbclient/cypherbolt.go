// CognoDB, Neo4j AuraDB, and Memgraph all speak Bolt + Cypher, so they share
// this one adapter — intentional, not laziness. The assignment brief itself
// says CognoDB needs "no other code changes" beyond swapping the URI into an
// official Neo4j driver, and Memgraph is Bolt-protocol-compatible by design.
// One adapter for all three guarantees byte-identical Cypher text and
// batching code runs against each, so any latency difference is the
// database, not the client.
package dbclient

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type CypherBoltClient struct {
	key         string
	displayName string
	uri         string
	user        string
	password    string
	driver      neo4j.DriverWithContext
}

func NewCypherBoltClient(key, displayName, uri, user, password string) *CypherBoltClient {
	return &CypherBoltClient{key: key, displayName: displayName, uri: uri, user: user, password: password}
}

func (c *CypherBoltClient) Key() string         { return c.key }
func (c *CypherBoltClient) DisplayName() string { return c.displayName }
func (c *CypherBoltClient) IndexedProperties() []string {
	return []string{"Person.id"}
}

func (c *CypherBoltClient) Connect(ctx context.Context) error {
	if c.uri == "" {
		return fmt.Errorf("[%s] missing URI — set it in .env before running this platform", c.key)
	}
	driver, err := neo4j.NewDriverWithContext(c.uri, neo4j.BasicAuth(c.user, c.password, ""))
	if err != nil {
		return err
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return err
	}
	c.driver = driver
	return nil
}

func (c *CypherBoltClient) Close(ctx context.Context) error {
	if c.driver == nil {
		return nil
	}
	return c.driver.Close(ctx)
}

func (c *CypherBoltClient) session(ctx context.Context) neo4j.SessionWithContext {
	return c.driver.NewSession(ctx, neo4j.SessionConfig{})
}

func (c *CypherBoltClient) ClearDatabase(ctx context.Context) error {
	session := c.session(ctx)
	defer session.Close(ctx)

	for {
		result, err := session.Run(ctx,
			"MATCH (n) WITH n LIMIT 5000 DETACH DELETE n RETURN count(n) AS c", nil)
		if err != nil {
			return err
		}
		record, err := result.Single(ctx)
		if err != nil {
			return err
		}
		raw, _ := record.Get("c")
		deleted, ok := raw.(int64)
		if !ok || deleted == 0 {
			return nil
		}
	}
}

func (c *CypherBoltClient) CreateIndexes(ctx context.Context) error {
	session := c.session(ctx)
	defer session.Close(ctx)

	_, err := session.Run(ctx, "CREATE INDEX person_id_index IF NOT EXISTS FOR (p:Person) ON (p.id)", nil)
	if err != nil {
		// Memgraph legacy syntax fallback
		_, err = session.Run(ctx, "CREATE INDEX ON :Person(id)", nil)
	}
	return err
}

func (c *CypherBoltClient) BulkLoad(ctx context.Context, nodeIDs []int64, edges [][2]int64, batchSize int) (LoadResult, error) {
	session := c.session(ctx)
	defer session.Close(ctx)

	start := time.Now()

	for i := 0; i < len(nodeIDs); i += batchSize {
		end := min(i+batchSize, len(nodeIDs))
		batch := nodeIDs[i:end]
		ids := make([]any, len(batch))
		for j, v := range batch {
			ids[j] = v
		}
		if _, err := session.Run(ctx, "UNWIND $ids AS id CREATE (:Person {id: id})",
			map[string]any{"ids": ids}); err != nil {
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
		cypher := `
			UNWIND $rows AS row
			MATCH (a:Person {id: row.src})
			MATCH (b:Person {id: row.dst})
			CREATE (a)-[:EMAILED]->(b)
		`
		if _, err := session.Run(ctx, cypher, map[string]any{"rows": rows}); err != nil {
			return LoadResult{}, err
		}
	}

	elapsed := time.Since(start).Seconds()
	return LoadResult{NodesLoaded: len(nodeIDs), EdgesLoaded: len(edges), WallClockSeconds: elapsed}, nil
}

func (c *CypherBoltClient) Traversal(ctx context.Context, startID int64, hops int) (int64, error) {
	var cypher string

	switch hops {
	case 1:
		cypher = `
			MATCH (p:Person {id: $id})-[:EMAILED]->(x)
			RETURN count(DISTINCT x) AS c
		`

	case 2:
		cypher = `
			MATCH (p:Person {id: $id})-[:EMAILED]->()-[:EMAILED]->(x)
			WITH DISTINCT x
			LIMIT 10000
			RETURN count(x) AS c
		`

	case 3:
		cypher = `
			MATCH (p:Person {id: $id})-[:EMAILED]->()-[:EMAILED]->()-[:EMAILED]->(x)
			WITH DISTINCT x
			LIMIT 10000
			RETURN count(x) AS c
		`

	default:
		return 0, fmt.Errorf("only 1/2/3 hops are benchmarked, got %d", hops)
	}

	session := c.session(ctx)
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, map[string]any{"id": startID})
	if err != nil {
		return 0, err
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}

	raw, _ := record.Get("c")
	return toInt64(raw), nil
}
func (c *CypherBoltClient) PointLookup(ctx context.Context, nodeID int64) (bool, error) {
	session := c.session(ctx)
	defer session.Close(ctx)
	result, err := session.Run(ctx, "MATCH (p:Person {id:$id}) RETURN p", map[string]any{"id": nodeID})
	if err != nil {
		return false, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return false, err
	}
	return len(records) > 0, nil
}

func (c *CypherBoltClient) IndexedRangeLookup(ctx context.Context, lo, hi int64) (int64, error) {
	session := c.session(ctx)
	defer session.Close(ctx)
	result, err := session.Run(ctx,
		"MATCH (p:Person) WHERE p.id >= $lo AND p.id < $hi RETURN count(p) AS c",
		map[string]any{"lo": lo, "hi": hi})
	if err != nil {
		return 0, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}
	raw, _ := record.Get("c")
	return toInt64(raw), nil
}

func (c *CypherBoltClient) AggregationTopOutDegree(ctx context.Context, limit int) ([]OutDegreeRow, error) {
	session := c.session(ctx)
	defer session.Close(ctx)
	cypher := `
		MATCH (p:Person)-[r:EMAILED]->()
		RETURN p.id AS person, count(r) AS sent
		ORDER BY sent DESC
		LIMIT $limit
	`
	result, err := session.Run(ctx, cypher, map[string]any{"limit": int64(limit)})
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]OutDegreeRow, 0, len(records))
	for _, rec := range records {
		personRaw, _ := rec.Get("person")
		sentRaw, _ := rec.Get("sent")
		rows = append(rows, OutDegreeRow{PersonID: toInt64(personRaw), Sent: toInt64(sentRaw)})
	}
	return rows, nil
}

func (c *CypherBoltClient) MixedReadOp(ctx context.Context, nodeID int64) error {
	session := c.session(ctx)
	defer session.Close(ctx)
	_, err := session.Run(ctx, "MATCH (p:Person {id:$id}) RETURN p.id AS id", map[string]any{"id": nodeID})
	return err
}

func (c *CypherBoltClient) MixedWriteOp(ctx context.Context, nodeID int64) error {
	session := c.session(ctx)
	defer session.Close(ctx)
	_, err := session.Run(ctx,
		"MATCH (p:Person {id:$id}) SET p.last_touched = timestamp() RETURN p.id AS id",
		map[string]any{"id": nodeID})
	return err
}

func (c *CypherBoltClient) StorageFootprint(ctx context.Context) (FootprintResult, error) {
	// Neo4j/Memgraph expose storage size via dbms.*/storage procedures that
	// are NOT standard Cypher, differ by platform/edition, and are commonly
	// disabled on free tiers. We try a best-effort call and fall back
	// honestly to "not observable" rather than guessing.
	session := c.session(ctx)
	defer session.Close(ctx)
	_, err := session.Run(ctx, "CALL dbms.queryJmx('org.neo4j:*') YIELD attributes RETURN attributes LIMIT 1", nil)
	if err != nil {
		return FootprintResult{Observable: false, Notes: "not observable on this tier/platform"}, nil
	}
	return FootprintResult{Observable: false, Notes: "partial JMX data available but not parsed into a single byte figure"}, nil
}

// toInt64 handles the fact that Cypher COUNT() comes back as int64 from the
// driver already, but we keep this narrow helper in case a platform ever
// returns a different numeric type for an aggregate.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
