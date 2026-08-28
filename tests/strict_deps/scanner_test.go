// Gazelle writes the deps; the check rejects the imports they do not cover.
// A specifier only one of them recognises is either drift Gazelle cannot fix
// (the check demands a dep Gazelle never generates) or drift the build never
// notices, so the two recognisers are pinned against one expectation here.
package strict_deps_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	typescript "github.com/mikn/rules_typescript/gazelle"
)

var scannerCases = []struct {
	name   string
	source string
	want   []string
	// Quoted module names in positions that are not imports. Declared to the
	// checker exactly like the wanted ones, so recognising one is a failure.
	decoys []string
}{
	{
		name:   "static forms",
		source: "import { a } from \"acme-named\";\nimport type { B } from \"acme-type\";\nimport * as ns from \"acme-star\";\nimport def from \"acme-default\";\nimport \"acme-side-effect\";\n",
		want:   []string{"acme-named", "acme-type", "acme-star", "acme-default", "acme-side-effect"},
	},
	{
		name:   "default and namespace in one clause",
		source: "import def, * as ns from \"acme-both\";\nexport const x = [def, ns];\n",
		want:   []string{"acme-both"},
	},
	{
		name:   "default and named in one clause",
		source: "import def, { named } from \"acme-mixed\";\nexport const x = [def, named];\n",
		want:   []string{"acme-mixed"},
	},
	{
		name:   "re-export forms",
		source: "export * from \"acme-star-reexport\";\nexport * as ns from \"acme-ns-reexport\";\nexport { a } from \"acme-named-reexport\";\nexport type { B } from \"acme-type-reexport\";\n",
		want:   []string{"acme-star-reexport", "acme-ns-reexport", "acme-named-reexport", "acme-type-reexport"},
	},
	{
		name:   "multi-line clause",
		source: "import {\n  a,\n  b,\n} from \"acme-multiline\";\n",
		want:   []string{"acme-multiline"},
	},
	{
		name:   "dynamic and require",
		source: "const a = import(\"acme-dynamic\");\nconst b = require(\"acme-require\");\nimport c = require(\"acme-legacy\");\n",
		want:   []string{"acme-dynamic", "acme-require", "acme-legacy"},
	},
	{
		name:   "import attributes",
		source: "import data from \"acme-attributes\" with { type: \"json\" };\n",
		want:   []string{"acme-attributes"},
	},
	{
		name:   "specifier behind a comment",
		source: "const a = import(/* chunk */ \"acme-commented\");\n// import { x } from \"acme-in-comment\";\n/* import \"acme-in-block\"; */\n",
		want:   []string{"acme-commented"},
		decoys: []string{"acme-in-comment", "acme-in-block"},
	},
	{
		name:   "object key named from",
		source: "const o = { from: \"acme-object-key\" };\nexport const v = o.from;\n",
		decoys: []string{"acme-object-key"},
	},
	{
		name:   "ambient module declaration",
		source: "declare module \"acme-ambient\" {\n  export const x: number;\n}\n",
		decoys: []string{"acme-ambient"},
	},
	{
		// A triple-slash directive resolves through TypeScript's type-reference
		// resolver, not module resolution, so neither side may treat it as a
		// specifier: the build would demand a dep Gazelle cannot name, since
		// types="x" means either @types/x or x and nothing here can choose.
		name:   "triple-slash reference directive",
		source: "/// <reference types=\"acme-types-ref\" />\n/// <reference path=\"./acme-path-ref.d.ts\" />\nexport const v = 1;\n",
		decoys: []string{"acme-types-ref", "./acme-path-ref.d.ts"},
	},
	{
		name:   "template literal",
		source: "export const s = `import { x } from \"acme-template\"`;\n",
		decoys: []string{"acme-template"},
	},
	{
		name:   "regex literal",
		source: "export const re = /from \"acme-regex\"/;\nimport { a } from \"acme-after-regex\";\n",
		want:   []string{"acme-after-regex"},
		decoys: []string{"acme-regex"},
	},
	{
		name:   "require.resolve is not require",
		source: "export const p = require.resolve(\"acme-resolve\");\n",
		decoys: []string{"acme-resolve"},
	},
	{
		name:   "a string that merely follows a word",
		source: "export default \"acme-default-export\";\nconst from = 1;\nexport const s = `${from}` + \"acme-plain\";\n",
		decoys: []string{"acme-default-export", "acme-plain"},
	},
}

var reFinding = regexp.MustCompile(`imports "([^"]+)"`)

func TestGazelleAndTheCheckerRecogniseTheSameImports(t *testing.T) {
	c := newChecker(t)

	for _, tc := range scannerCases {
		t.Run(tc.name, func(t *testing.T) {
			fromGazelle := specifiers(typescript.ScanImports(tc.source))
			assertSameSet(t, "gazelle", fromGazelle, tc.want)

			// Every candidate is declared transitively available, so the
			// checker reports exactly the specifiers it recognised.
			var manifest []string
			for _, name := range append(append([]string{}, tc.want...), tc.decoys...) {
				manifest = append(manifest, moduleTransitive(name, "//pkg:"+name))
			}
			out, ok := c.run(strings.NewReplacer(" ", "_", ".", "_").Replace(tc.name), tc.source, manifest...)
			if ok && len(tc.want) > 0 {
				t.Fatalf("the checker recognised none of %v:\n%s", tc.want, out)
			}
			var fromChecker []string
			for _, m := range reFinding.FindAllStringSubmatch(out, -1) {
				fromChecker = append(fromChecker, m[1])
			}
			assertSameSet(t, "the checker", fromChecker, tc.want)
		})
	}
}

func specifiers(imports []typescript.Import) []string {
	var out []string
	for _, imp := range imports {
		out = append(out, imp.Specifier)
	}
	return out
}

func assertSameSet(t *testing.T, who string, got, want []string) {
	t.Helper()
	normalise := func(in []string) []string {
		seen := map[string]struct{}{}
		var out []string
		for _, s := range in {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}
	g, w := normalise(got), normalise(want)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s recognised %v, want %v", who, g, w)
	}
}
