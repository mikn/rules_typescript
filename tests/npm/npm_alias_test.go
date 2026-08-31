package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// `nano-alias: npm:nanoid@3.3.11` means `import "nano-alias"` must find
// node_modules/nano-alias. That only exists if the alias gets its own target with
// package_name set to the alias: package_name is what the tree builder writes on
// disk, so a Bazel alias pointing at nanoid's own target writes node_modules/nanoid.
//
// The two routes an alias arrives by are checked separately, each with a fixture
// entry that only the one route reaches.
func TestAliasInstallsUnderItsAliasName(t *testing.T) {
	tree := verify.New(t)

	for _, c := range []struct {
		tree, alias, via string
	}{
		// Declared by the workspace root and by nothing else, so the label exists
		// only if importers are read for aliases as well as links.
		{"importer_alias_node_modules", "ms-alias", "the root importer"},
		// Declared by zod and by nothing else, so it exists only if the dependency
		// edge carries the name zod imports nanoid under.
		{"package_alias_node_modules", "nano-alias", "zod's dependency edge"},
		// Pinned through a catalog, and resolved against a peer set, so the
		// lockfile records it under a `snapshots:` key rather than a bare
		// name@version.
		{"catalog_alias_node_modules", "styles-alias", "a catalog entry with peers"},
	} {
		nm := tree.FoundDir("*/" + c.tree)
		if !nm.File(c.alias + "/package.json").Exists() {
			t.Errorf("%s has no %s/ directory (alias reached via %s)", nm.Name(), c.alias, c.via)
		}
	}
}
