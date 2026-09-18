package jsx_preserve_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// tsc names a .tsx's emit .jsx under jsx: preserve; the JSX in it is left for
// the bundler, and nothing lands at the .js name a bundler refuses to parse.
func TestPreserveEmitsWhatTscEmits(t *testing.T) {
	tree := verify.New(t)

	view := tree.File("tests/jsx_preserve/view.jsx")
	view.Contains(`<p id="root">`, "export function view(label)")
	tree.File("tests/jsx_preserve/view.jsx.map").Exists()
	tree.File("tests/jsx_preserve/view.d.ts").Contains(
		"export declare function view(label: string)",
	)
	tree.Absent("tests/jsx_preserve/view.js")

	// A consumer's specifier is left as written; the runtime resolves it to
	// the .jsx the way it resolves an extensionless import to a .js.
	tree.File("tests/jsx_preserve/uses_view.js").Contains(`from "./view"`)
}
