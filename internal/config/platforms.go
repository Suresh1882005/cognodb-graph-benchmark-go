package config

// PlatformSpec is the static, hand-maintained description of one platform:
// which adapter it uses, its advertised resource tier, and the honest
// caveats around that tier. This is the single source of truth — mirrored
// into docs for humans, but this struct is what the code actually reads.
//
// IMPORTANT: values marked [VERIFY AT SIGNUP] must be confirmed against the
// live console when you actually provision each instance, then updated here
// and in README.md before submission.
type PlatformSpec struct {
	Key            string
	DisplayName    string
	Dialect        string // "cypher" | "cypher_subset" | "aql"
	EnvPrefix      string // e.g. "COGNODB" -> reads COGNODB_URI, COGNODB_USER, COGNODB_PASSWORD
	AdvertisedVCPU string
	AdvertisedRAM  string
	AdvertisedDisk string
	TierName       string
	SpecSource     string
	Notes          string
}

// Registry is the ordered list of platforms this benchmark compares.
// Order here is the order they're benchmarked and reported in by default.
var Registry = []PlatformSpec{
	{
		Key:            "cognodb",
		DisplayName:    "CognoDB Cloud (c0 free tier)",
		Dialect:        "cypher",
		EnvPrefix:      "COGNODB",
		AdvertisedVCPU: "0.5 (burstable)",
		AdvertisedRAM:  "256 MB",
		AdvertisedDisk: "1 GB",
		TierName:       "c0 (free)",
		SpecSource:     "assignment brief",
		Notes:          "Reference tier — every other platform is sized to match this as closely as its own tier allows.",
	},
	{
		Key:            "neo4j_aura",
		DisplayName:    "Neo4j AuraDB (Free tier)",
		Dialect:        "cypher",
		EnvPrefix:      "NEO4J",
		AdvertisedVCPU: "not publicly disclosed [VERIFY AT SIGNUP]",
		AdvertisedRAM:  "not publicly disclosed [VERIFY AT SIGNUP]",
		AdvertisedDisk: "not publicly disclosed [VERIFY AT SIGNUP]",
		TierName:       "AuraDB Free",
		SpecSource:     "https://neo4j.com/cloud/platform/aura-graph-database/faq/ (checked 2026-08-12)",
		Notes: "Neo4j does not publish exact vCPU/RAM for the Free tier. Two Neo4j-affiliated " +
			"sources even disagree on the node/relationship cap (200k/400k vs 50k/175k) — confirm the " +
			"real number for your own instance in the Aura console and record it in the README. This " +
			"asymmetry is documented, not hidden.",
	},
	{
		Key:            "memgraph",
		DisplayName:    "Memgraph",
		Dialect:        "cypher",
		EnvPrefix:      "MEMGRAPH",
		AdvertisedVCPU: "0.5 (docker --cpus=0.5, matches CognoDB)",
		AdvertisedRAM:  "256 MB",
		AdvertisedDisk: "1 GB",
		TierName:       "self-hosted, capped via docker-compose.yml",
		SpecSource:     "docker-compose.yml resource limits in this repo",
		Notes: "Preferred: try Memgraph Cloud's free trial first and use its real specs. " +
			"Fallback: the self-hosted container in docker-compose.yml, explicitly capped to CognoDB's " +
			"specs. Either path is allowed by the assignment brief.",
	},
	{
		Key:            "falkordb",
		DisplayName:    "FalkorDB",
		Dialect:        "cypher_subset",
		EnvPrefix:      "FALKORDB",
		AdvertisedVCPU: "0.5 (docker --cpus=0.5, matches CognoDB)",
		AdvertisedRAM:  "256 MB",
		AdvertisedDisk: "1 GB",
		TierName:       "self-hosted, capped via docker-compose.yml",
		SpecSource:     "docker-compose.yml resource limits in this repo",
		Notes: "Preferred: FalkorDB Cloud free tier if available at signup time. Fallback: self-hosted " +
			"container capped to match CognoDB. FalkorDB speaks an openCypher subset over the Redis wire " +
			"protocol (not Bolt), via the official falkordb-go client rather than the neo4j driver — a " +
			"genuine, disclosed protocol/dialect difference, not an oversight.",
	},
	{
		Key:            "arangodb",
		DisplayName:    "ArangoDB",
		Dialect:        "aql",
		EnvPrefix:      "ARANGODB",
		AdvertisedVCPU: "0.5 (docker --cpus=0.5, matches CognoDB)",
		AdvertisedRAM:  "512 MB (see caveat)",
		AdvertisedDisk: "1 GB",
		TierName:       "self-hosted, capped via docker-compose.yml",
		SpecSource:     "docker-compose.yml resource limits in this repo",
		Notes: "ArangoDB is multi-model and queried in AQL, not Cypher — the harness issues logically " +
			"equivalent AQL for every workload. CAVEAT: ArangoDB's minimum stable memory footprint is " +
			"higher than CognoDB's 256 MB; we run it at 512 MB and disclose the deviation rather than " +
			"report numbers from a container that OOM-kills mid-run. Prefer ArangoDB Oasis's free trial " +
			"with its real specs if you can get one running inside the 48-hour window.",
	},
}

// ByKey returns a platform's spec, or ok=false if unknown.
func ByKey(key string) (PlatformSpec, bool) {
	for _, p := range Registry {
		if p.Key == key {
			return p, true
		}
	}
	return PlatformSpec{}, false
}

// Keys returns every registered platform key, in registry order.
func Keys() []string {
	keys := make([]string, 0, len(Registry))
	for _, p := range Registry {
		keys = append(keys, p.Key)
	}
	return keys
}
