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
		dts.Contains(field + ":")
	}
	dts.Contains(`"name": string`, `"port": number`, `"debug": boolean`)

	// A JSON module is not readonly, and a declaration that says otherwise
	// rejects assignments the same file accepts under resolveJsonModule.
	dts.Excludes("readonly ")
}

// The shapes TypeScript treats differently from one another. Sampling v[0] for
// an array's element type answered every one of them wrong, silently: the
// erased keys produce no diagnostic, they produce a smaller type.
func TestArrayElementUnion(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/json/shapes.json.d.ts")

	// A key a later element carries survives, as the optional-undefined member
	// TypeScript itself writes for a union of object literals.
	dts.Contains(`"b"?: undefined;`, `"b": number;`)
	dts.Contains(`"q"?: undefined;`, `"q": number;`)

	dts.Contains(`"empty": never[];`)
	dts.Contains(`"obj": {};`)
	dts.MatchesRE(`"mixed": \((?:number|string|boolean)(?: \| (?:number|string|boolean)){2}\)\[\];`)
	dts.Contains(`| null)[];`)
	dts.Excludes("readonly ")
}

// TypeScript reads JSON as JSONC, and so does Gazelle. A file the ruleset can
// generate a target for is a file the rule has to be able to read.
func TestJSONCDeclaration(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/json/commented.json.d.ts")

	dts.Contains(`"name": string`, `"retries": number`, `"enabled": boolean`)
	dts.Excludes(": unknown")
}
