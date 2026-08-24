package bundle_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestCompileOutputsAndBinaryRunfiles(t *testing.T) {
	tree := verify.New(t)

	for _, rel := range []string{
		"tests/bundle/lib.js", "tests/bundle/lib.d.ts", "tests/bundle/lib.js.map",
		"tests/bundle/app.js", "tests/bundle/app.d.ts", "tests/bundle/app.js.map",
	} {
		tree.File(rel).Exists()
	}

	tree.File("tests/bundle/lib.js").Contains("function greet")
	tree.File("tests/bundle/lib.d.ts").Contains("greet")

	// app.ts compiles against lib's .d.ts, so app.js keeps importing it.
	tree.File("tests/bundle/app.js").Contains("import", "message")
}
