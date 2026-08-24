package compile_layout_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// A strip prefix taken from one src's directory puts the other tree's outputs at
// the wrong depth, and a rootDir taken the same way makes tsgo write the
// declarations outside the directory Bazel declared them in.
func TestSiblingDirectoryLayout(t *testing.T) {
	tree := verify.New(t)

	for _, rel := range []string{
		"alpha/one.js", "alpha/one.js.map", "alpha/one.d.ts", "alpha/one.d.ts.map",
		"alpha/three.js", "alpha/three.d.ts",
		"beta/deep/two.js", "beta/deep/two.d.ts",
	} {
		tree.File("tests/compile_layout/" + rel).Exists()
	}

	// Nothing may land at a depth the package-relative path does not name.
	for _, stray := range []string{"one.js", "alpha/alpha/one.js", "alpha/beta/deep/two.js", "deep/two.js"} {
		tree.Absent("tests/compile_layout/" + stray)
	}

	// The specifiers survive the transform, and both resolve against the layout
	// above.
	one := tree.File("tests/compile_layout/alpha/one.js")
	one.Contains(`"./three.js"`, `"../beta/deep/two.js"`)

	// The declaration is the proof the .js specifiers resolved: noEmitOnError is
	// on, so an unresolved import leaves no .d.ts behind.
	tree.File("tests/compile_layout/alpha/one.d.ts").Contains("one")
}
