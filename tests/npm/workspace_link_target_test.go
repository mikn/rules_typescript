package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Each member's compiled entry sits at the path its manifest names, behind the
// link the features root declares for it.
func TestWorkspaceLinkTargetFiles(t *testing.T) {
	v := verify.New(t)
	nm := v.FoundDir("*/features/node_modules")
	for _, member := range []string{"boundary-member", "leaf-member", "exports-member"} {
		if !nm.File(member + "/src/index.js").Exists() {
			t.Errorf("%s/src/index.js is not behind the member's link", member)
		}
	}
}
