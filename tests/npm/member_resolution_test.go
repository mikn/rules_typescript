package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The path after the last node_modules/ is the member's store tree: the link
// the codegen's deps staged entered it.
func TestCodegenResolvesMemberLink(t *testing.T) {
	tree := verify.New(t)
	tree.File("tests/npm/member_resolution.ts").Contains(
		`export const shared: string = "shared/src/index.js";`,
		`export const shared_wire: string = "shared/src/wire/index.js";`,
	)
}
