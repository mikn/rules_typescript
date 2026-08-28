package lint_real_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The stamp is only written when the linter exits 0, so its presence is the
// assertion that a real oxlint ran and passed on clean.ts.
func TestRealLintStamp(t *testing.T) {
	verify.New(t).File("tests/lint_real/clean_lint.tslint").Exists()
}
