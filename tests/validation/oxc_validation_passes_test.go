package validation_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// Under --//ts:declarations=oxc the stamp is written only when tsgo exits 0;
// under the tsgo default the .d.ts are the proof (declaration_types_test).
func TestTsgoCheckStamp(t *testing.T) {
	verify.New(t).File("tests/validation/annotated.tscheck").Exists()
}
