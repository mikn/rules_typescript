package js_sources_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/js_sources/"

// JavaScript needs no transform, so it is staged in the output tree unchanged;
// what it does need is to be in the type program, or a relative import of it
// from TypeScript is TS2307 and its JSDoc types stop at the file boundary.
func TestJavaScriptSourcesAreStagedAndTyped(t *testing.T) {
	tree := verify.New(t)

	// Staged next to the compiled TypeScript, so "./math.js" resolves at runtime.
	tree.File(pkg + "math.js").Contains("function add")
	tree.File(pkg + "main.js").Exists()

	// The declaration tsgo emits for a JavaScript source carries the JSDoc types.
	tree.File(pkg+"math.d.ts").Contains("a: number", "b: number")

	// tsc's naming for the two module-specific extensions.
	tree.File(pkg + "esm.d.mts").Contains("flavour")
	tree.File(pkg + "legacy.d.cts").Contains("flavour")
	tree.File(pkg + "esm.mjs").Exists()
	tree.File(pkg + "legacy.cjs").Exists()
}
