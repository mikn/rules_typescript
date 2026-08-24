package validation_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Under declarations = "oxc" the checking is a validation action, and the stamp
// is written only when tsgo exits 0. Under the "tsgo" default there is no stamp
// -- the .d.ts themselves are the proof, covered by declaration_types_test.
func TestTsgoCheckStamp(t *testing.T) {
	verify.New(t).File("tests/validation/annotated_oxc.tscheck").Exists()
}
