package npm_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// A package.json src is staged as built at the package's path: the source
// targets name the emitted files, a .tsx the .jsx its tsconfig declares.
func TestManifestStagedAsBuilt(t *testing.T) {
	tree := verify.New(t)
	tree.File("packages/shared/package.json").Contains(
		`"exports":{".":"./src/index.js","./wire":"./src/wire/index.js"}`,
		`"type":"module"`,
	)
	tree.File("tests/jsx_preserve/member/package.json").Contains(
		`"exports":"./view.jsx"`,
	)
}
