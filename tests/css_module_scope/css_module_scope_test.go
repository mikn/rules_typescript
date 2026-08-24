package css_module_scope_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestLocalAndGlobalScope(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/css_module_scope/scope.module.css.d.ts")

	for _, class := range []string{"foo", "mixLocal", "mixPlain", "innerLocal", "blockLocal", "reLocal", "mediaLocal"} {
		dts.Contains("readonly " + class + ": string")
	}
	for _, class := range []string{"bar", "mixGlobal", "outerGlobal", "blockGlobal", "mediaGlobal"} {
		dts.ExcludesRE(`readonly "?` + class + `"?:`)
	}
}
