package source_maps_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/source_maps/"

// declaration_map is what makes go-to-definition from a consumer's editor land
// on mapped.ts instead of the generated mapped.d.ts.
func TestDeclarationMapPointsAtTheSource(t *testing.T) {
	tree := verify.New(t)

	tree.File(pkg + "mapped.d.ts.map").Contains("mapped.ts")
	tree.File(pkg + "mapped.d.ts").Contains("sourceMappingURL=mapped.d.ts.map")
	tree.File(pkg + "mapped.js.map").Exists()
}

func TestSourceMapDisabledEmitsNoJSMap(t *testing.T) {
	tree := verify.New(t)

	tree.File(pkg + "unmapped.js").Exists()
	tree.Absent(pkg + "unmapped.js.map")
}
