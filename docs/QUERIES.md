# Exact queries per platform

This file exists so a reviewer never has to reverse-engineer what was
actually measured — every query below is copy-pasted from the adapters in
`internal/dbclient/`, not paraphrased.

Data model (identical on every platform): `(:Person {id: INT})-[:EMAILED]->(:Person {id: INT})`

## Index / constraint creation

| Platform | Statement |
|---|---|
| CognoDB / Neo4j AuraDB / Memgraph | `CREATE INDEX person_id_index IF NOT EXISTS FOR (p:Person) ON (p.id)` (falls back to `CREATE INDEX ON :Person(id)` for older Memgraph syntax if that errors) |
| FalkorDB | `CREATE INDEX FOR (p:Person) ON (p.id)` |
| ArangoDB | `personsCol.EnsurePersistentIndex(ctx, []string{"id"}, &arangodb.CreatePersistentIndexOptions{Unique: &unique})` (driver call, not AQL) |

## 1 / 2 / 3-hop traversal (example: 2-hop)

**Cypher (CognoDB, Neo4j AuraDB, Memgraph, FalkorDB):**
```cypher
MATCH (p:Person {id:$id})-[:EMAILED]->()-[:EMAILED]->(x)
RETURN count(DISTINCT x) AS c
LIMIT 10000
```

**AQL (ArangoDB):**
```aql
FOR v IN 2..2 OUTBOUND @start_vertex emailed
  LIMIT 10000
  COLLECT WITH COUNT INTO c
  RETURN c
```

The `LIMIT 10000` cap is applied identically on every platform. Without it, a
handful of very high out-degree Enron nodes (mail-server-like accounts) would
make 2/3-hop reachable sets balloon into the hundreds of thousands, which
would make the benchmark measure "how fast can you enumerate a huge result
set" rather than "how fast is traversal" — the cap is a fairness decision,
not a performance trick for any one platform.

## Point lookup

**Cypher:** `MATCH (p:Person {id:$id}) RETURN p`
**AQL:** `FOR p IN persons FILTER p.id == @id RETURN p`

## Indexed / filtered (range) lookup

**Cypher:** `MATCH (p:Person) WHERE p.id >= $lo AND p.id < $hi RETURN count(p) AS c`
**AQL:** `FOR p IN persons FILTER p.id >= @lo AND p.id < @hi COLLECT WITH COUNT INTO c RETURN c`

`hi = lo + 1000` in every run — a fixed-width ~1000-id window anchored on a
random point, so the selectivity is comparable across platforms and runs.

## Aggregation (top-10 by out-degree)

**Cypher:**
```cypher
MATCH (p:Person)-[r:EMAILED]->()
RETURN p.id AS person, count(r) AS sent
ORDER BY sent DESC
LIMIT 10
```

**AQL:**
```aql
FOR p IN persons
  LET sent = LENGTH(FOR v, e IN 1..1 OUTBOUND p emailed RETURN 1)
  SORT sent DESC
  LIMIT 10
  RETURN {person: p.id, sent: sent}
```

## Mixed workload primitives

- **Read:** `MATCH (p:Person {id:$id}) RETURN p.id AS id` (AQL equivalent: `FOR p IN persons FILTER p.id == @id RETURN p.id`)
- **Write:** `MATCH (p:Person {id:$id}) SET p.last_touched = timestamp() RETURN p.id AS id` — a property update on an existing node, chosen so repeated runs never grow the dataset (no new nodes/edges are created or deleted during the mixed workload, so ingest and traversal numbers stay comparable run to run).

## Where this lives in code

| Platform | Adapter file |
|---|---|
| CognoDB / Neo4j AuraDB / Memgraph | `internal/dbclient/cypherbolt.go` |
| FalkorDB | `internal/dbclient/falkordb.go` |
| ArangoDB | `internal/dbclient/arangodb.go` |

Every adapter implements the same `GraphClient` interface defined in
`internal/dbclient/client.go` — that interface's doc comments are the other
half of this transparency story: they explain *why* each method exists, not
just what it queries.
