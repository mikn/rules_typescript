package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Each member's compiled entry point at the path its manifest names, `src/index`,
// whichever directory holds the target that compiled it: the view's root is the
// member's directory, so the rolled-up member and the two per-directory ones lay
// out alike. A hub label that reached a different target in any of those
// directories would stage none of these files.
func TestWorkspaceLinkTargetFiles(t *testing.T) {
	v := verify.New(t)
	v.FoundFile("*/workspace_link_target_node_modules/boundary-member/src/index.js").Exists()
	v.FoundFile("*/workspace_link_target_node_modules/leaf-member/src/index.js").Exists()
	v.FoundFile("*/workspace_link_target_node_modules/exports-member/src/index.js").Exists()
}
