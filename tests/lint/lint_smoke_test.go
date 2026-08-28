package lint_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The stamp is only written when the linter exits 0, so its presence is the
// assertion.
func TestLintStamp(t *testing.T) {
	verify.New(t).File("tests/lint/clean_lint.tslint").Exists()
}
