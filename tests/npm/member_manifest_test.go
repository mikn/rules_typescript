package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// A package.json src is staged as written; the manifest as built beside it,
// <name>.package.json, names the emitted files, a .tsx the .jsx it declares.
func TestManifestAsWrittenAndAsBuilt(t *testing.T) {
	tree := verify.New(t)
	tree.File("packages/shared/package.json").Contains(
		`"./src/index.ts"`,
		`"./src/wire/index.ts"`,
	)
	tree.File("packages/shared/shared.package.json").Contains(
		`"exports":{".":"./src/index.js","./wire":"./src/wire/index.js"}`,
		`"type":"module"`,
	)
	tree.File("tests/jsx_preserve/member/package.json").Contains(
		`"exports": "./view.tsx"`,
	)
	tree.File("tests/jsx_preserve/member/member.package.json").Contains(
		`"exports":"./view.jsx"`,
	)
}
