package codegen_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestGeneratedFile(t *testing.T) {
	tree := verify.New(t)
	tree.File("tests/codegen/generated.ts").Contains("export const GENERATED = true")
}
