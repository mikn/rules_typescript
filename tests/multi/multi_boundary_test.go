package multi_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// tests/multi/app imports from tests/multi/lib. The compiled app.js must import
// rather than inline the lib source, which is what proves app compiled against
// lib's .d.ts and not its .ts.
func TestDeclarationBoundary(t *testing.T) {
	tree := verify.New(t)

	for _, rel := range []string{"lib/math", "app/main"} {
		tree.File("tests/multi/" + rel + ".js").Exists()
		tree.File("tests/multi/" + rel + ".d.ts").Exists()
		tree.File("tests/multi/" + rel + ".js.map").Exists()
	}

	tree.File("tests/multi/lib/math.d.ts").Contains(
		"export declare function add(a: number, b: number): number;",
		"export declare function multiply(a: number, b: number): number;",
		"export interface MathResult {",
	)

	main := tree.File("tests/multi/app/main.js")
	main.Contains("export function calculate(x, y)")
	// A bare "import" check would also pass on an import of something else, so
	// match the specifier too. The lib function bodies must not appear at all --
	// that would be bundling.
	main.Contains(`import { add, multiply } from "../lib/math"`)
	main.Excludes("return a + b", "return a * b")

	tree.File("tests/multi/app/main.d.ts").Contains(
		"export declare function calculate(x: number, y: number): number;",
	)
}
