package css_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestCSSDepsCompile(t *testing.T) {
	tree := verify.New(t)

	// button.css comes from its own css_library; theme.css only through the
	// transitive css_library dep.
	tree.File("tests/css/button.css").Contains(".button")
	tree.File("tests/css/theme.css").Contains("color-primary")

	// A CSS import must not break the TypeScript compile.
	tree.File("tests/css/Button.js").Contains("describeButton")
	tree.File("tests/css/Button.js.map").Exists()
	tree.File("tests/css/Button.d.ts").Contains("ButtonProps", "describeButton")
}
