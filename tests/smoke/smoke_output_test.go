package smoke_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestCompiledOutputs(t *testing.T) {
	tree := verify.New(t)

	// oxc strips types, so the annotations from hello.ts must be gone from the
	// .js while the .d.ts carries them in full.
	js := tree.File("tests/smoke/hello.js")
	js.Contains("function hello()", `return "hello"`, `GREETING = "world"`)
	js.Excludes("function hello(): string", "GREETING: string")
	tree.File("tests/smoke/hello.js.map").Exists()

	tree.File("tests/smoke/hello.d.ts").Contains(
		"export declare function hello(): string;",
		"export declare const GREETING: string;",
	)

	// Source file is Button.tsx so the output stem preserves the case.
	button := tree.File("tests/smoke/Button.js")
	button.Contains("function Button(props)")
	// jsx_mode = "react-jsx": the element becomes a jsx() call against the
	// automatic runtime import, and no angle-bracket syntax survives.
	button.Contains(`from "react/jsx-runtime"`, `_jsx("button"`)
	button.Excludes("<button")
	tree.File("tests/smoke/Button.js.map").Exists()

	// The whole ButtonProps shape has to survive into the declaration, members
	// included -- an empty `interface ButtonProps {}` satisfies a name-only check.
	tree.File("tests/smoke/Button.d.ts").Contains(
		"export interface ButtonProps {",
		"label: string;",
		"onClick: () => void;",
		"export declare function Button(props: ButtonProps): JSX.Element;",
	)
}
