package js_declarations_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/js_declarations/"

// Both targets built, so "./esm.mjs" resolved -- in :types_only to the only
// file that could answer, in :with_runtime to the declaration rather than the
// JavaScript main.ts refuses. What is left is the output tree.
func TestDeclarationFlavoursInSrcs(t *testing.T) {
	tree := verify.New(t)

	for _, compiled := range []string{"typed.js", "main.js", "consumer.js", "uses_global.js"} {
		tree.File(pkg + compiled).Exists()
	}

	// The JavaScript is staged as-is, so the relative import resolves at runtime.
	tree.File(pkg + "esm.mjs").Contains("function flavour")
	tree.File(pkg + "legacy.cjs").Contains("legacyFlavour")

	// The declaration beside each is the checked-in one: `value: string`, where
	// tsgo's emit for the untyped JavaScript would have said `value: any`.
	tree.File(pkg + "esm.d.mts").Contains("flavour(value: string)")
	tree.File(pkg + "legacy.d.cts").Contains("legacyFlavour(value: string)")
}
