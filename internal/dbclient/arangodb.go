// ArangoDB is multi-model and queried in AQL, not Cypher — this adapter
// issues logically equivalent AQL for every workload defined in client.go.
// It's included deliberately to show the harness handles a genuinely
// different query language honestly, rather than only comparing four
// Cypher-flavoured databases and calling that "five platforms."
//
// Graph model: two collections, `persons` (vertices) and `emailed` (edge
// collection), matching the same (:Person)-[:EMAILED]->(:Person) semantics
// used everywhere else in this repo.
package dbclient

import (
	"context"
	"fmt"
	"time"

	"github.com/arangodb/go-driver/v2/arangodb"
	"github.com/arangodb/go-driver/v2/connection"
)

const (
	personsCollection = "persons"
	emailedCollection = "emailed"
)

type ArangoDBClient struct {
	uri      string
	user     string
	password string
	dbName   string
	client   arangodb.Client
	db       arangodb.Database
}

func NewArangoDBClient(uri, user, password, dbName string) *ArangoDBClient {
	if dbName == "" {
		dbName = "benchmark"
	}
	return &ArangoDBClient{uri: uri, user: user, password: password, dbName: dbName}
}

func (c *ArangoDBClient) Key() string         { return "arangodb" }
func (c *ArangoDBClient) DisplayName() string { return "ArangoDB" }
func (c *ArangoDBClient) IndexedProperties() []string {
	return []string{"persons.id (persistent index)"}
}

func (c *ArangoDBClient) Connect(ctx context.Context) error {
	if c.uri == "" {
		return fmt.Errorf("[arangodb] missing ARANGODB_URI — set it in .env before running this platform")
	}
	endpoint := connection.NewRoundRobinEndpoints([]string{c.uri})
	conn := connection.NewHttp2Connection(connection.DefaultHTTP2ConfigurationWrapper(endpoint, true))
	if err := conn.SetAuthentication(connection.NewBasicAuth(c.user, c.password)); err != nil {
		return err
	}
	client := arangodb.NewClient(conn)

	exists, err := client.DatabaseExists(ctx, c.dbName)
	if err != nil {
		return err
	}
	var db arangodb.Database
	if !exists {
		db, err = client.CreateDatabase(ctx, c.dbName, nil)
	} else {
		db, err = client.GetDatabase(ctx, c.dbName, nil)
	}
	if err != nil {
		return err
	}

	c.client = client
	c.db = db
	return nil
}

func (c *ArangoDBClient) Close(_ context.Context) error {
	return nil // python-arango-style stateless HTTP client, nothing to close
}

func (c *ArangoDBClient) ClearDatabase(ctx context.Context) error {
	for _, name := range []string{emailedCollection, personsCollection} {
		exists, err := c.db.CollectionExists(ctx, name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		col, err := c.db.GetCollection(ctx, name, nil)
		if err != nil {
			return err
		}
		if err := col.Remove(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *ArangoDBClient) CreateIndexes(ctx context.Context) error {
	personsCol, err := c.ensureCollection(ctx, personsCollection, arangodb.CollectionTypeDocument)
	if err != nil {
		return err
	}
	if _, err := c.ensureCollection(ctx, emailedCollection, arangodb.CollectionTypeEdge); err != nil {
		return err
	}

	unique := true
	_, _, err = personsCol.EnsurePersistentIndex(ctx, []string{"id"}, &arangodb.CreatePersistentIndexOptions{
		Unique: &unique,
	})
	return err
}

func (c *ArangoDBClient) ensureCollection(ctx context.Context, name string, colType arangodb.CollectionType) (arangodb.Collection, error) {
	exists, err := c.db.CollectionExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if exists {
		return c.db.GetCollection(ctx, name, nil)
	}
	return c.db.CreateCollectionV2(ctx, name, &arangodb.CreateCollectionPropertiesV2{Type: &colType})
}

func (c *ArangoDBClient) BulkLoad(ctx context.Context, nodeIDs []int64, edges [][2]int64, batchSize int) (LoadResult, error) {
	personsCol, err := c.db.GetCollection(ctx, personsCollection, nil)
	if err != nil {
		return LoadResult{}, err
	}
	emailedCol, err := c.db.GetCollection(ctx, emailedCollection, nil)
	if err != nil {
		return LoadResult{}, err
	}

	start := time.Now()

	for i := 0; i < len(nodeIDs); i += batchSize {
		end := min(i+batchSize, len(nodeIDs))
		batch := nodeIDs[i:end]
		docs := make([]map[string]any, len(batch))
		for j, n := range batch {
			docs[j] = map[string]any{"_key": fmt.Sprintf("%d", n), "id": n}
		}
		reader, err := personsCol.CreateDocuments(ctx, docs)
		if err != nil {
			return LoadResult{}, err
		}
		if _, errs := reader.ReadAll(); len(errs) > 0 && hasNonNilErr(errs) {
			return LoadResult{}, fmt.Errorf("person batch insert: %v", firstNonNilErr(errs))
		}
	}

	for i := 0; i < len(edges); i += batchSize {
		end := min(i+batchSize, len(edges))
		batch := edges[i:end]
		docs := make([]map[string]any, len(batch))
		for j, e := range batch {
			docs[j] = map[string]any{
				"_from": fmt.Sprintf("%s/%d", personsCollection, e[0]),
				"_to":   fmt.Sprintf("%s/%d", personsCollection, e[1]),
			}
		}
		reader, err := emailedCol.CreateDocuments(ctx, docs)
		if err != nil {
			return LoadResult{}, err
		}
		if _, errs := reader.ReadAll(); len(errs) > 0 && hasNonNilErr(errs) {
			return LoadResult{}, fmt.Errorf("edge batch insert: %v", firstNonNilErr(errs))
		}
	}

	elapsed := time.Since(start).Seconds()
	return LoadResult{NodesLoaded: len(nodeIDs), EdgesLoaded: len(edges), WallClockSeconds: elapsed}, nil
}

func (c *ArangoDBClient) Traversal(ctx context.Context, startID int64, hops int) (int64, error) {
	if hops != 1 && hops != 2 && hops != 3 {
		return 0, fmt.Errorf("only 1/2/3 hops are benchmarked, got %d", hops)
	}
	aql := fmt.Sprintf(`
		FOR v IN %d..%d OUTBOUND @start_vertex %s
		  LIMIT 10000
		  COLLECT WITH COUNT INTO c
		  RETURN c
	`, hops, hops, emailedCollection)
	return c.aqlScalarCount(ctx, aql, map[string]any{
		"start_vertex": fmt.Sprintf("%s/%d", personsCollection, startID),
	})
}

func (c *ArangoDBClient) PointLookup(ctx context.Context, nodeID int64) (bool, error) {
	aql := fmt.Sprintf("FOR p IN %s FILTER p.id == @id RETURN p", personsCollection)
	cursor, err := c.db.Query(ctx, aql, &arangodb.QueryOptions{BindVars: map[string]any{"id": nodeID}})
	if err != nil {
		return false, err
	}
	defer cursor.Close()
	return cursor.HasMore(), nil
}

func (c *ArangoDBClient) IndexedRangeLookup(ctx context.Context, lo, hi int64) (int64, error) {
	aql := fmt.Sprintf(
		"FOR p IN %s FILTER p.id >= @lo AND p.id < @hi COLLECT WITH COUNT INTO c RETURN c",
		personsCollection)
	return c.aqlScalarCount(ctx, aql, map[string]any{"lo": lo, "hi": hi})
}

func (c *ArangoDBClient) AggregationTopOutDegree(ctx context.Context, limit int) ([]OutDegreeRow, error) {
	aql := fmt.Sprintf(`
		FOR p IN %s
		  LET sent = LENGTH(FOR v, e IN 1..1 OUTBOUND p %s RETURN 1)
		  SORT sent DESC
		  LIMIT @limit
		  RETURN {person: p.id, sent: sent}
	`, personsCollection, emailedCollection)
	cursor, err := c.db.Query(ctx, aql, &arangodb.QueryOptions{BindVars: map[string]any{"limit": limit}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close()

	var rows []OutDegreeRow
	for cursor.HasMore() {
		var row struct {
			Person int64 `json:"person"`
			Sent   int64 `json:"sent"`
		}
		if _, err := cursor.ReadDocument(ctx, &row); err != nil {
			return nil, err
		}
		rows = append(rows, OutDegreeRow{PersonID: row.Person, Sent: row.Sent})
	}
	return rows, nil
}

func (c *ArangoDBClient) MixedReadOp(ctx context.Context, nodeID int64) error {
	aql := fmt.Sprintf("FOR p IN %s FILTER p.id == @id RETURN p.id", personsCollection)
	cursor, err := c.db.Query(ctx, aql, &arangodb.QueryOptions{BindVars: map[string]any{"id": nodeID}})
	if err != nil {
		return err
	}
	defer cursor.Close()
	return nil
}

func (c *ArangoDBClient) MixedWriteOp(ctx context.Context, nodeID int64) error {
	aql := fmt.Sprintf(`
		FOR p IN %s FILTER p.id == @id
		  UPDATE p WITH {last_touched: DATE_NOW()} IN %s
		  RETURN NEW.id
	`, personsCollection, personsCollection)
	cursor, err := c.db.Query(ctx, aql, &arangodb.QueryOptions{BindVars: map[string]any{"id": nodeID}})
	if err != nil {
		return err
	}
	defer cursor.Close()
	return nil
}

func (c *ArangoDBClient) StorageFootprint(ctx context.Context) (FootprintResult, error) {
	// python-arango exposes collection.statistics()['figures']['documentsSize'];
	// go-driver/v2 doesn't currently surface an equivalent typed figures call,
	// so we report honestly rather than parsing an undocumented raw endpoint.
	return FootprintResult{Observable: false, Notes: "not observable via go-driver/v2 on this tier/platform"}, nil
}

// aqlScalarCount runs a COLLECT WITH COUNT INTO c ... RETURN c style query
// and returns that single count, or 0 if the result set is empty.
func (c *ArangoDBClient) aqlScalarCount(ctx context.Context, aql string, bindVars map[string]any) (int64, error) {
	cursor, err := c.db.Query(ctx, aql, &arangodb.QueryOptions{BindVars: bindVars})
	if err != nil {
		return 0, err
	}
	defer cursor.Close()
	if !cursor.HasMore() {
		return 0, nil
	}
	var count int64
	if _, err := cursor.ReadDocument(ctx, &count); err != nil {
		return 0, err
	}
	return count, nil
}

func hasNonNilErr(errs []error) bool {
	for _, e := range errs {
		if e != nil {
			return true
		}
	}
	return false
}

func firstNonNilErr(errs []error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
