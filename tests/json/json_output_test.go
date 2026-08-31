package json_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestJSONDeclaration(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/json/config.json.d.ts")

	dts.MatchesRE(`(?m)^declare const data:`, `(?m)^export default data`)

	// Inference failed if anything came out as `unknown`.
	dts.Excludes(": unknown")

	for _, field := range []string{`"name"`, `"port"`, `"debug"`, `"database"`} {
		dts.Contains("readonly " + field + ":")
	}
	dts.Contains(`"name": string`, `"port": number`, `"debug": boolean`)
}

// TypeScript reads JSON as JSONC, and so does Gazelle. A file the ruleset can
// generate a target for is a file the rule has to be able to read.
func TestJSONCDeclaration(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/json/commented.json.d.ts")

	dts.Contains(`"name": string`, `"retries": number`, `"enabled": boolean`)
	dts.Excludes(": unknown")
}
