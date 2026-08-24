package subtree_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/subtree/"

// Before oxc was invoked once per source root with the package as the strip
// prefix, --strip-dir-prefix came from srcs[0].dirname, so whichever level that
// file sat on decided the layout for every other file.
func TestSubtreeKeepsPackageRelativePaths(t *testing.T) {
	tree := verify.New(t)

	for _, rel := range []string{
		"root.js", "root.d.ts",
		"nested/helper.js", "nested/helper.d.ts",
		"nested/deep/leaf.js", "nested/deep/leaf.d.ts",
	} {
		tree.File(pkg + rel).Exists()
	}

	// The depths a strip prefix read off srcs[0] ("nested/deep") would produce.
	for _, stray := range []string{"helper.js", "leaf.js", "deep/leaf.js", "nested/nested/helper.js"} {
		tree.Absent(pkg + stray)
	}

	// The ".js" specifiers must survive into the output, where they resolve
	// against the .js files above.
	tree.File(pkg+"root.js").Contains(`"./nested/helper.js"`, `"./nested/deep/leaf.js"`)
}
