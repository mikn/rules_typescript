package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Each member's compiled entry sits at the path its manifest names, whichever
// directory compiles it; a hub label on another target would stage none.
func TestWorkspaceLinkTargetFiles(t *testing.T) {
	v := verify.New(t)
	v.FoundFile("*/workspace_link_target_node_modules/boundary-member/src/index.js").Exists()
	v.FoundFile("*/workspace_link_target_node_modules/leaf-member/src/index.js").Exists()
	v.FoundFile("*/workspace_link_target_node_modules/exports-member/src/index.js").Exists()
}
