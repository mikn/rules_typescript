package css_module_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestModuleDeclaration(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/css_module/Button.module.css.d.ts")

	dts.Contains("declare const styles", "export default styles")

	for _, class := range []string{"container", "button", "label"} {
		dts.Contains("readonly " + class + ": string")
	}
	// A compound class (.button.disabled) and a class reachable only through a
	// nested at-rule are both still class names.
	dts.Contains("readonly disabled: string", `readonly "media-only": string`)

	// Names that are not valid TS identifiers must be quoted, or the .d.ts does
	// not parse.
	dts.Contains(`readonly "button-primary": string`)

	// Nothing outside a selector is a class name.
	for _, notAClass := range []string{"png", "fake-class", "no-scope", "spin-frames", "5em"} {
		dts.ExcludesRE(`readonly "?` + notAClass + `"?:`)
	}
}
