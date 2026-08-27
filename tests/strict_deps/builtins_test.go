// Gazelle drops a builtin instead of writing a label; the check resolves the
// same names against Node's own `builtinModules`. A name only Node knows makes
// Gazelle write `@npm//:<name>`, a label no hub declares -- so the hand list is
// pinned against the node the check actually runs, and a node_version bump that
// adds a bare builtin fails here rather than in a generated BUILD file.
package strict_deps_test

import (
	"os/exec"
	"strings"
	"testing"

	typescript "github.com/mikn/rules_typescript/gazelle"
	"github.com/mikn/rules_typescript/tests/verify"
)

func TestGazelleKnowsEveryBuiltinTheCheckerDoes(t *testing.T) {
	tree := verify.New(t)
	node := tree.File("ts/toolchain/node_resolved/node")
	if !node.Exists() {
		t.FailNow()
	}

	out, err := exec.Command(node.Abs(), "-p", `require("node:module").builtinModules.join("\n")`).Output()
	if err != nil {
		t.Fatalf("listing builtinModules: %v", err)
	}

	var fromNode []string
	for _, name := range strings.Fields(string(out)) {
		// A prefix-only module never reaches a label: resolveNpmPackage
		// answers on the prefix before any name is consulted.
		if strings.HasPrefix(name, "node:") {
			continue
		}
		fromNode = append(fromNode, barePackageName(name))
	}

	assertSameSet(t, "gazelle's builtin list", typescript.NodeBuiltins(), fromNode)
}

// barePackageName is the mapping Gazelle applies before the list is consulted:
// "fs/promises" is the builtin "fs".
func barePackageName(specifier string) string {
	return strings.SplitN(specifier, "/", 2)[0]
}
