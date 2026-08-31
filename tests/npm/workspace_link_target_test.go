package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Each member's compiled entry point, at the path its own target puts it: the
// rolled-up member keeps the src/ prefix its ts_compile rolled up, the two
// per-directory ones do not. A hub label that reached a different target in any
// of those directories would stage none of these files.
func TestWorkspaceLinkTargetFiles(t *testing.T) {
	v := verify.New(t)
	v.FoundFile("*/workspace_link_target_node_modules/boundary-member/src/index.js").Exists()
	v.FoundFile("*/workspace_link_target_node_modules/leaf-member/index.js").Exists()
	v.FoundFile("*/workspace_link_target_node_modules/exports-member/index.js").Exists()
}
