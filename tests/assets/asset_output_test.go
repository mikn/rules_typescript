package assets_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestAssetDeclaration(t *testing.T) {
	tree := verify.New(t)

	tree.File("tests/assets/logo.svg.d.ts").Contains(
		"declare const asset: string",
		"export default asset",
	)
	tree.File("tests/assets/logo.svg").Exists()
}
