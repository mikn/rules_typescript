package css_module_scope_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestLocalAndGlobalScope(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/css_module_scope/scope.module.css.d.ts")

	// blockGlobal is here because postcss-modules scopes a class inside a
	// `:global { }` BLOCK locally anyway -- only the `:global(...)` group form
	// leaves a name alone.
	for _, class := range []string{"foo", "mixLocal", "mixPlain", "blockLocal", "blockGlobal", "reLocal", "mediaLocal"} {
		dts.Contains("readonly " + class + ": string")
	}
	for _, class := range []string{"bar", "mixGlobal", "mediaGlobal"} {
		dts.ExcludesRE(`readonly "?` + class + `"?:`)
	}
}

// The .d.ts is written from the export map, so an option that renames a key has
// to be set where the rule can see it.
func TestOptionsReachTheCompiler(t *testing.T) {
	tree := verify.New(t)

	exports := map[string]string{}
	tree.File("tests/css_module_scope/options.module.css.exports.json").JSON(&exports)

	if got := exports["kebabCased"]; got != "_kebab-cased_d4d806c5" {
		t.Errorf("locals_convention/hash_prefix: kebabCased = %q", got)
	}
	if got := exports["leftAlone"]; got != "left-alone" {
		t.Errorf("export_globals: leftAlone = %q, want the unscoped name", got)
	}

	dts := tree.File("tests/css_module_scope/options.module.css.d.ts")
	dts.Contains("readonly kebabCased: string", "readonly leftAlone: string")
	dts.ExcludesRE(`readonly "kebab-cased"`)
}
