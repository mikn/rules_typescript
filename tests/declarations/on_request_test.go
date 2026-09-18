package declarations_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The `declarations` output group requested, TsgoDeclare writes lib.d.ts with
// the return type tsgo inferred from the whole program.
func TestDeclarationsOnRequest(t *testing.T) {
	verify.New(t).File("tests/declarations/lib.d.ts").
		Contains("export declare function double(n: number): number;")
}
