package validation_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Regression test for the failure mode that motivated tsgo declaration emit: a
// syntactic emitter faced with un-annotated exports produces `{}` and `unknown`,
// the target still builds, and only a downstream consumer fails -- pointing at
// the wrong file.
func TestInferredDeclarations(t *testing.T) {
	tree := verify.New(t)

	dts := tree.File("tests/validation/inferred.d.ts")
	dts.Contains("RegExp")
	dts.ExcludesRE(`:\s*\{\}`, `:\s*unknown`)
	// The inferred function return type must be the string-literal union, not
	// string.
	dts.Contains(`"preview"`)

	tree.File("tests/validation/correct.d.ts").Contains("declare function add")
}
