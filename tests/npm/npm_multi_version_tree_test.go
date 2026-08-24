package npm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const nodeModules = "node_modules"

// resolveFrom is Node's module resolution restricted to one tree: from the
// directory holding an importing file, try <dir>/node_modules/<name> at every
// ancestor. The tree root stands in for a node_modules directory of its own,
// which is what it is wherever these trees are mounted. Returns the resolved
// directory relative to the tree root, or "".
func resolveFrom(root, from, name string) string {
	for dir := from; ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) != nodeModules {
			if candidate := filepath.Join(dir, nodeModules, name); isPackage(candidate) {
				return candidate
			}
		}
		if dir == root {
			if candidate := filepath.Join(dir, name); isPackage(candidate) {
				return candidate
			}
			return ""
		}
	}
}

func isPackage(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "package.json"))
	return err == nil
}

// One name resolving to two versions in one closure is not an edge case: pnpm
// records each resolution separately and every one of them is real. A tree that
// keys a directory by name alone has one place to put them, so the second
// version copied overwrites the first and every dependent gets whichever won --
// silently, because the version that survives is a real version of the real
// package.
//
// Both chains below run three deep through the same three names and disagree at
// every level, so a name-keyed tree hands one of them the other's closure.
//
// The tree arrives here as a test action's input, materialised from the
// TreeArtifact rather than read where it was written, which is the part the
// version-keyed layout depends on: the links into .pnpm are relative and
// internal to the tree, so they have to travel with it.
func TestEachDependentResolvesItsOwnVersion(t *testing.T) {
	tree := verify.New(t)

	for _, fixture := range []struct {
		nodeModules string
		chains      [][2][]string
	}{
		{"multi_version_node_modules", [][2][]string{
			{{"test-exclude", "minimatch", "brace-expansion", "balanced-match"},
				{"7.0.2", "10.2.4", "5.0.4", "4.0.4"}},
			// glob is in the same closure and pins the older major of all three,
			// which is the chain a name-keyed tree loses.
			{{"glob", "minimatch", "brace-expansion", "balanced-match"},
				{"10.5.0", "9.0.9", "2.0.2", "1.0.2"}},
		}},
		// A scoped name, whose store directory is one path segment while the name
		// inside it is two.
		{"scoped_multi_version_node_modules", [][2][]string{
			{{"vitest", "@vitest/pretty-format"}, {"3.0.9", "3.2.4"}},
			{{"@vitest/snapshot", "@vitest/pretty-format"}, {"3.0.9", "3.0.9"}},
			{{"@vitest/utils", "@vitest/pretty-format"}, {"3.0.9", "3.0.9"}},
		}},
	} {
		nm := tree.FoundDir("*/" + fixture.nodeModules)
		root := nm.Abs()
		for _, chain := range fixture.chains {
			names, versions := chain[0], chain[1]
			dir := root
			for i, name := range names {
				resolved := resolveFrom(root, dir, name)
				if resolved == "" {
					t.Errorf("%s: %v does not resolve %q: nothing by that name up the tree from %s",
						fixture.nodeModules, names[:i], name, rel(t, root, dir))
					break
				}
				var pkg struct {
					Version string `json:"version"`
				}
				at := rel(t, root, resolved)
				nm.File(at + "/package.json").JSON(&pkg)
				if pkg.Version != versions[i] {
					t.Errorf("%s: %v resolves %q to %s@%s (at %s), want %s@%s",
						fixture.nodeModules, names[:i], name, name, pkg.Version, at, name, versions[i])
				}
				dir = resolved
			}
		}
	}
}

func rel(t *testing.T, root, path string) string {
	t.Helper()
	out, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("relativising %s against %s: %v", path, root, err)
	}
	return filepath.ToSlash(out)
}
