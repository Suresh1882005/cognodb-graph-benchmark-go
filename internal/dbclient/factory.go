package dbclient

import (
	"fmt"

	"github.com/suresh/cognodb-graph-benchmark/internal/config"
)

// Build constructs the right GraphClient implementation for a platform key,
// reading that platform's <PREFIX>_* environment variables via cfg.
func Build(platformKey string, cfg *config.Env) (GraphClient, error) {
	spec, ok := config.ByKey(platformKey)
	if !ok {
		return nil, fmt.Errorf("unknown platform %q (known: %v)", platformKey, config.Keys())
	}
	env := cfg.PlatformVars(spec.EnvPrefix)

	switch spec.Dialect {
	case "cypher":
		return NewCypherBoltClient(spec.Key, spec.DisplayName, env["URI"], env["USER"], env["PASSWORD"]), nil
	case "cypher_subset":
		return NewFalkorDBClient(env["HOST"], env["PORT"], env["PASSWORD"]), nil
	case "aql":
		return NewArangoDBClient(env["URI"], env["USER"], env["PASSWORD"], env["DB"]), nil
	default:
		return nil, fmt.Errorf("no adapter registered for dialect %q (platform %q)", spec.Dialect, platformKey)
	}
}
