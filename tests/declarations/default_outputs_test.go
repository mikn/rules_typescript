package declarations_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// A data dep on :lib stages its default outputs: the .js and its map, and no
// declaration, so nothing in this test's build ran the leaf's declare.
func TestDefaultOutputsHoldNoDeclaration(t *testing.T) {
	tree := verify.New(t)
	tree.File("tests/declarations/lib.js").Exists()
	tree.File("tests/declarations/lib.js.map").Exists()
	tree.Absent("tests/declarations/lib.d.ts")
}
