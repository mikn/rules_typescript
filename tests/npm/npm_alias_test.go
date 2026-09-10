package npm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// An alias is a link under the alias name into the aliased tree: at the
// importer's node_modules when it declares it, beside the dependent otherwise.
func TestAliasInstallsUnderItsAliasName(t *testing.T) {
	tree := verify.New(t)
	nm := tree.FoundDir("*/features/node_modules")

	// Declared by the features root and by nothing else, so the link exists
	// only if importers are read for aliases as well as links.
	if !nm.File("ms-alias/package.json").Exists() {
		t.Errorf("%s has no ms-alias link (alias reached via the root importer)", nm.Name())
	}
	// Pinned through a catalog and resolved against a peer set: a `snapshots:`
	// key, not a bare name@version.
	if !nm.File("styles-alias/package.json").Exists() {
		t.Errorf("%s has no styles-alias link (alias reached via a catalog entry with peers)", nm.Name())
	}
	// Declared by zod and by nothing else, so it exists only if the dependency
	// edge carries the name zod imports nanoid under.
	if _, err := os.Stat(filepath.Join(beside(t, filepath.Join(nm.Abs(), "zod"), "nano-alias"), "package.json")); err != nil {
		t.Errorf("zod's tree has no nano-alias link beside it (alias reached via zod's dependency edge): %v", err)
	}
}
