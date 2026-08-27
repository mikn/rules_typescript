package css_module_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestModuleDeclaration(t *testing.T) {
	tree := verify.New(t)
	dts := tree.File("tests/css_module/Button.module.css.d.ts")

	dts.Contains("declare const styles", "export default styles")

	for _, class := range []string{"container", "button", "label"} {
		dts.Contains("readonly " + class + ": string")
	}
	// A compound class (.button.disabled) and a class reachable only through a
	// nested at-rule are both still class names.
	dts.Contains("readonly disabled: string", `readonly "media-only": string`)

	// Names that are not valid TS identifiers must be quoted, or the .d.ts does
	// not parse.
	dts.Contains(`readonly "button-primary": string`)

	// postcss-modules scopes the @keyframes name too, so it is a real export.
	dts.Contains(`readonly "spin-frames": string`)

	// Nothing outside a selector is a class name.
	for _, notAClass := range []string{"png", "fake-class", "no-scope", "5em"} {
		dts.ExcludesRE(`readonly "?` + notAClass + `"?:`)
	}
}

var declaration = regexp.MustCompile(`readonly (?:"([^"]+)"|([A-Za-z0-9_$]+)): string`)

// The .d.ts is generated from the export map rather than from a second reading
// of the CSS, so the two key sets are the same set or the generator is lying.
func TestDeclarationKeysAreTheExportMapKeys(t *testing.T) {
	tree := verify.New(t)

	exports := map[string]string{}
	tree.File("tests/css_module/Button.module.css.exports.json").JSON(&exports)

	var want []string
	for name := range exports {
		want = append(want, name)
	}
	sort.Strings(want)

	var got []string
	for _, m := range declaration.FindAllStringSubmatch(tree.File("tests/css_module/Button.module.css.d.ts").Text(), -1) {
		got = append(got, m[1]+m[2])
	}
	sort.Strings(got)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the .d.ts declares %v, the export map holds %v", got, want)
	}
}

// A scoped name is a content hash and nothing else: no path, no line number.
func TestScopedNamesAreContentAddressed(t *testing.T) {
	tree := verify.New(t)

	exports := map[string]string{}
	tree.File("tests/css_module/Button.module.css.exports.json").JSON(&exports)

	var hashes []string
	for name, scoped := range exports {
		want := regexp.MustCompile(`^_` + regexp.QuoteMeta(name) + `_([0-9a-f]{8})$`)
		m := want.FindStringSubmatch(scoped)
		if m == nil {
			t.Fatalf("%s maps to %q, want _<name>_<8 hex>", name, scoped)
		}
		hashes = append(hashes, m[1])
	}
	for _, h := range hashes {
		if h != hashes[0] {
			t.Errorf("one stylesheet, one hash: got %v", hashes)
			break
		}
	}
}
