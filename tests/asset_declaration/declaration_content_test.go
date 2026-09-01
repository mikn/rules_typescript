package assetdeclaration_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestDeclarationType(t *testing.T) {
	tree := verify.New(t)

	tree.File("tests/asset_declaration/mark.svg.d.ts").Contains("declare const asset: string")

	tree.File("tests/asset_declaration/icon.svg.d.ts").Contains(
		"declare const asset: (props: { className?: string }) => null",
		"//tests/asset_declaration:icon_svg",
		`declaration_type[".svg"]`,
	)

	tree.File("tests/asset_declaration/badge.svg.d.ts").Contains(
		"declare const asset: (props: { className?: string }) => null",
	)
	tree.File("tests/asset_declaration/note.txt.d.ts").Contains(
		"declare const asset: { readonly words: number }",
	)

	// One target, one extension mapped: the other keeps the default.
	tree.File("tests/asset_declaration/hero.svg.d.ts").Contains(
		"declare const asset: (props: { className?: string }) => null",
	)
	tree.File("tests/asset_declaration/photo.png.d.ts").Contains("declare const asset: string")

	// Retyping does not stop the asset itself being staged for the bundler.
	tree.File("tests/asset_declaration/icon.svg").Exists()
	tree.File("tests/asset_declaration/hero.svg").Exists()
	tree.File("tests/asset_declaration/photo.png").Exists()
}
