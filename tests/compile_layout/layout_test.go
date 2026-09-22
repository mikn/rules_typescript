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

// A data src is staged at its package-relative path, so a relative reference
// from the compiled module beside it resolves at run time as it did in source.
func TestDataSrcsAreStagedBesideTheirModule(t *testing.T) {
	tree := verify.New(t)

	for _, rel := range []string{
		"gamma/index.js", "gamma/index.d.ts",
		"gamma/README.md", "gamma/data.json", "gamma/logo.svg",
		"gamma/package.json", "gamma/styles.css",
	} {
		tree.File("tests/compile_layout/" + rel).Exists()
	}

	// The declaration is the proof the JSON import resolved: data.json was a tsgo
	// input, and its type was read under bundler resolution's resolveJsonModule.
	tree.File("tests/compile_layout/gamma/index.d.ts").
		Contains("export declare const name: string;")
}
