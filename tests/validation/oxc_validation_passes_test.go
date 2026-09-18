package validation_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The stamp is written only when tsgo exits 0 and the listing checks out.
func TestTsgoCheckStamp(t *testing.T) {
	verify.New(t).File("tests/validation/annotated.tscheck").Exists()
}
