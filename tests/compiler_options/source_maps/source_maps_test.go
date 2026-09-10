package source_maps_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const pkg = "tests/compiler_options/source_maps/"

// --//ts:declaration_map is what makes go-to-definition from a consumer's editor land
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

// Either emitter's map names the src by its exec-root path and carries its
// text; tsgo wrote commonjs.js.map two directories deeper, in a scratch outDir.
func TestMapSourcesNameTheSourceByItsExecRootPath(t *testing.T) {
	tree := verify.New(t)

	for _, stem := range []string{"mapped", "commonjs"} {
		var m struct {
			Sources        []string `json:"sources"`
			SourcesContent []string `json:"sourcesContent"`
		}
		tree.File(pkg + stem + ".js.map").JSON(&m)
		want := []string{pkg + stem + ".ts"}
		if !reflect.DeepEqual(m.Sources, want) {
			t.Errorf("%s.js.map sources = %q, want %q", stem, m.Sources, want)
		}
		text := strings.Join(m.SourcesContent, "")
		if len(m.SourcesContent) != 1 || !strings.Contains(text, "export const") {
			t.Errorf("%s.js.map sourcesContent = %q, want the source", stem, text)
		}
	}
}
