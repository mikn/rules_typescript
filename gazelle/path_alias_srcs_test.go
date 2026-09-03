package typescript

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
	bzl "github.com/bazelbuild/buildtools/build"
)

// A test file is a program of its own: the alias map on the package's
// ts_compile reaches nothing the test compiles, so an alias the test's imports
// go through has to be on the ts_test. Until it was, every aliased import in a
// generated test failed with TS2307 and the attribute had to be written by hand.
func TestGenerate_TsTestCarriesTheAliasesItsSourcesUse(t *testing.T) {
	res := runGenerateWithBuild(t, "app", `
# gazelle:ts_path_alias @/ app/
# gazelle:ts_path_alias ~ui/ packages/ui/src/
# gazelle:ts_path_alias ~unused/ packages/unused/
`, map[string]string{
		"main.ts":      "export const y = 1;\n",
		"main.test.ts": "import type { Button } from '~ui/button';\nimport { y } from './main';\nexport const t: Button = y;\n",
	})

	r := generatedRule(res, "app_test")
	if r == nil {
		t.Fatal("no ts_test named app_test")
	}
	want := map[string]string{"@/": "app/", "~ui/": "packages/ui/src/"}
	if got := pathAliasesOf(r); !reflect.DeepEqual(got, want) {
		t.Errorf("ts_test(app_test).path_aliases = %v, want %v: the alias its import goes "+
			"through and the one its own srcs sit under, and nothing else", got, want)
	}
}

// The two shapes measured on the monorepo trial. //web:web_test has srcs under
// web/shared/, so the alias validates on them and the aliased declarations
// arrive on the dep edge: path_alias_srcs there would stage every output of a
// 10,000-file target for nothing. //web:node_tooling_test has every src under
// plugins/, so nothing it stages sits under the alias and analysis fails until
// path_alias_srcs names the target that owns web/shared/. The same guard runs on
// a ts_compile, so the same rule decides for one.
var aliasSrcsWorkspace = map[string]string{
	"package.json":            convergePlainPkg,
	"tsconfig.json":           `{"compilerOptions":{"paths":{"#shared/*":["./src/shared/*"]}}}` + "\n",
	"src/shared/util.ts":      "export const util = 1;\nexport type Util = number;\n",
	"src/shared/util.test.ts": "import type { Util } from \"#shared/util\";\nexport const t: Util = 1;\n",
	"src/app/consumer.ts":     "import { util } from \"#shared/util\";\nexport const app = util;\n",
	"e2e/smoke.test.ts":       "import type { Util } from \"#shared/util\";\nexport const t: Util = 2;\n",
}

func TestPathAliasSrcs_OnlyWhenNoOwnSrcSitsUnderTheAlias(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, aliasSrcsWorkspace)
	captureLog(t, func() { convergeGazelle(t, root) })

	alias := map[string]string{"#shared/": "src/shared/"}

	covered := findRule(t, root, "src/shared", "ts_test", "shared_test")
	assertPathAliasesOn(t, covered, alias)
	if got := covered.AttrStrings("path_alias_srcs"); len(got) != 0 {
		t.Errorf("ts_test(shared_test).path_alias_srcs = %v; util.test.ts is under src/shared/, "+
			"so the alias validates on the test's own srcs and the target's outputs would be "+
			"staged for nothing:\n%s", got, indent(buildFileText(t, root, "src/shared")))
	}

	uncovered := findRule(t, root, "e2e", "ts_test", "e2e_test")
	assertPathAliasesOn(t, uncovered, alias)
	if got, want := uncovered.AttrStrings("path_alias_srcs"), []string{"//src/shared"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_test(e2e_test).path_alias_srcs = %v, want %v: no src of the test is under "+
			"src/shared/, so only the target the import resolved to can stage the alias:\n%s",
			got, want, indent(buildFileText(t, root, "e2e")))
	}

	compile := findRule(t, root, "src/app", "ts_compile", "app")
	assertPathAliasesOn(t, compile, alias)
	if got, want := compile.AttrStrings("path_alias_srcs"), []string{"//src/shared"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_compile(app).path_alias_srcs = %v, want %v: the guard is the same on a "+
			"ts_compile, and consumer.ts imports across the alias too:\n%s",
			got, want, indent(buildFileText(t, root, "src/app")))
	}

	// Gazelle wrote both attributes, so a second run over the same tree has
	// nothing to say and nothing to rewrite.
	before := map[string]string{}
	for _, pkg := range []string{"src/shared", "e2e", "src/app"} {
		before[pkg] = buildFileText(t, root, pkg)
	}
	logged := captureLog(t, func() { convergeGazelle(t, root) })
	if logged != "" {
		t.Errorf("the second run over an unchanged tree reported Gazelle's own output:\n%s", indentLog(logged))
	}
	for pkg, text := range before {
		if after := buildFileText(t, root, pkg); after != text {
			t.Errorf("the second run over an unchanged tree rewrote %s/BUILD.bazel:\n%s", pkg, lineDiff(text, after))
		}
	}
}

// Once Gazelle writes the map on a ts_test, the map is Gazelle's to correct --
// the same reason ts_config.deps became mergeable -- and the merge is the one
// entry-by-entry merge ts_compile's map already gets.
func TestTsTestPathAliasesMergeEntryByEntry(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, aliasSrcsWorkspace)
	captureLog(t, func() { convergeGazelle(t, root) })

	addAliasEntries(t, root, "e2e",
		`        "@kept/": "src/app/",  # keep`,
		`        "@bare/": "src/app/",`)

	for run := 2; run <= 3; run++ {
		logged := captureLog(t, func() { convergeGazelle(t, root) })
		got := pathAliasesOf(findRule(t, root, "e2e", "ts_test", "e2e_test"))
		if _, held := got["@kept/"]; !held {
			t.Fatalf("ts_test(e2e_test).path_aliases lost the hand-authored \"@kept/\" on run %d "+
				"even though it carries a \"# keep\":\n%s\nthe run said:\n%s",
				run, indent(buildFileText(t, root, "e2e")), indentLog(logged))
		}
		if _, held := got["@bare/"]; held {
			t.Fatalf("ts_test(e2e_test).path_aliases kept \"@bare/\" with no \"# keep\" on run %d; "+
				"Gazelle owns the attribute now, so an entry it cannot derive goes:\n%s",
				run, indent(buildFileText(t, root, "e2e")))
		}
		if got["#shared/"] != "src/shared/" {
			t.Fatalf("ts_test(e2e_test).path_aliases lost the entry Gazelle derives on run %d: %v", run, got)
		}
		if run == 2 && !strings.Contains(logged, `"@bare/"`) {
			t.Fatalf("path_aliases dropped the hand-authored \"@bare/\" and said nothing about "+
				"it:\n%s", indentLog(logged))
		}
		if run == 3 && logged != "" {
			t.Fatalf("run 3 reported path_aliases again, so the drop notice outlived its cause:\n%s",
				indentLog(logged))
		}
	}
}

func findRule(t *testing.T, root, pkg, kind, name string) *rule.Rule {
	t.Helper()
	for _, r := range loadRules(t, root, pkg) {
		if r.Kind() == kind && r.Name() == name {
			return r
		}
	}
	t.Fatalf("no %s(%s) in %s/BUILD.bazel:\n%s", kind, name, pkg, indent(buildFileText(t, root, pkg)))
	return nil
}

func pathAliasesOf(r *rule.Rule) map[string]string {
	d, isDict := r.Attr("path_aliases").(*bzl.DictExpr)
	if !isDict {
		return nil
	}
	out := map[string]string{}
	for _, kv := range d.List {
		k, keyOK := kv.Key.(*bzl.StringExpr)
		v, valueOK := kv.Value.(*bzl.StringExpr)
		if keyOK && valueOK {
			out[k.Value] = v.Value
		}
	}
	return out
}

func assertPathAliasesOn(t *testing.T, r *rule.Rule, want map[string]string) {
	t.Helper()
	if got := pathAliasesOf(r); !reflect.DeepEqual(got, want) {
		t.Errorf("%s(%s).path_aliases = %v, want %v", r.Kind(), r.Name(), got, want)
	}
}
