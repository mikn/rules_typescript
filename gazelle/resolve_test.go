package typescript

import (
	"reflect"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- helpers ---------------------------------------------------------------

// indexedRule is one rule to put in the RuleIndex: a kind, a target name, the
// Bazel package it lives in, and its srcs.
type indexedRule struct {
	kind       string
	name       string
	pkg        string
	srcs       []string
	moduleName string
}

func newRule(ir indexedRule) (*rule.Rule, *rule.File) {
	r := rule.NewRule(ir.kind, ir.name)
	if ir.srcs != nil {
		r.SetAttr("srcs", ir.srcs)
	}
	if ir.moduleName != "" {
		r.SetAttr("module_name", ir.moduleName)
	}
	return r, rule.EmptyFile("BUILD.bazel", ir.pkg)
}

// buildIndex indexes the given rules through the language's own Imports hook,
// which is what makes this exercise importsForRule and the resolver together.
func buildIndex(t *testing.T, c *config.Config, rules ...indexedRule) *resolve.RuleIndex {
	t.Helper()
	lang := &tsLang{}
	ix := resolve.NewRuleIndex(func(*rule.Rule, string) resolve.Resolver { return lang })
	for _, ir := range rules {
		r, f := newRule(ir)
		ix.AddRule(c, r, f)
	}
	ix.Finish()
	return ix
}

func emptyConfig() *config.Config {
	return &config.Config{RepoRoot: "/tmp/fake-repo", Exts: make(map[string]interface{})}
}

func specStrings(specs []resolve.ImportSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		if s.Lang != languageName {
			out = append(out, s.Lang+":"+s.Imp)
			continue
		}
		out = append(out, s.Imp)
	}
	return out
}

// ---- importsForRule --------------------------------------------------------

func TestImportsForRule_TsCompile(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_compile", name: "components", pkg: "src/components",
		srcs: []string{"index.ts", "Button.tsx", "helpers.ts"},
	})

	got := specStrings(importsForRule(c, r, f))
	// index.ts additionally makes the package directory itself importable.
	want := []string{
		"src/components/index",
		"src/components",
		"src/components/Button",
		"src/components/helpers",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importsForRule(ts_compile) = %v, want %v", got, want)
	}
}

func TestImportsForRule_ModuleNameIsIndexed(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_compile", name: "lib", pkg: "packages/lib",
		srcs: []string{"index.ts", "helpers.ts"}, moduleName: "@acme/lib",
	})

	got := specStrings(importsForRule(c, r, f))
	want := []string{
		"packages/lib/index",
		"packages/lib",
		"packages/lib/helpers",
		"@acme/lib",
		"@acme/lib/helpers",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importsForRule(module_name) = %v, want %v", got, want)
	}
}

func TestImportsForRule_TsTestIsIndexed(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_test", name: "math_test", pkg: "src/lib",
		srcs: []string{"math.test.ts"},
	})

	got := specStrings(importsForRule(c, r, f))
	want := []string{"src/lib/math.test"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importsForRule(ts_test) = %v, want %v", got, want)
	}
}

func TestImportsForRule_AssetKindsUseWorkspaceRelativeSrcs(t *testing.T) {
	c := emptyConfig()
	for _, kind := range []string{"css_library", "css_module", "asset_library", "json_library"} {
		r, f := newRule(indexedRule{
			kind: kind, name: "styles", pkg: "src/components",
			srcs: []string{"Button.module.css", "theme.css"},
		})
		got := specStrings(importsForRule(c, r, f))
		// The extension is kept: TypeScript imports these by their real filename.
		want := []string{"src/components/Button.module.css", "src/components/theme.css"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("importsForRule(%s) = %v, want %v", kind, got, want)
		}
	}
}

func TestImportsForRule_UnknownKindIsNotImportable(t *testing.T) {
	c := emptyConfig()
	for _, kind := range []string{"ts_bundle", "ts_lint", "filegroup"} {
		r, f := newRule(indexedRule{
			kind: kind, name: "thing", pkg: "src/app", srcs: []string{"index.ts"},
		})
		if got := importsForRule(c, r, f); got != nil {
			t.Errorf("importsForRule(%s) = %v, want nil (kind must not be indexed)", kind, got)
		}
	}
}

// ---- resolveImports (the deps attribute) -----------------------------------

func TestResolveImports_SetsSortedDedupedDeps(t *testing.T) {
	c := emptyConfig()
	tc := makeConfig("", nil)
	c.Exts[languageName] = tc

	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "src/lib", srcs: []string{"index.ts"}},
		indexedRule{kind: "ts_compile", name: "util", pkg: "src/util", srcs: []string{"index.ts"}},
	)

	r := rule.NewRule("ts_compile", "app")
	from := label.New("", "src/app", "app")
	// "zod" twice: the second must not produce a duplicate dep.
	imports := []string{"../lib", "zod", "../util", "zod"}
	resolveImports(c, ix, r, imports, from)

	got := r.AttrStrings("deps")
	want := []string{"//src/lib", "//src/util", "@npm//:zod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

func TestResolveImports_NoImportsLeavesDepsUnset(t *testing.T) {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", nil)
	ix := buildIndex(t, c)
	from := label.New("", "src/app", "app")

	for name, imports := range map[string]any{
		"nil":          nil,
		"empty slice":  []string{},
		"wrong type":   map[string]string{"a": "b"},
		"only builtin": []string{"node:fs"},
	} {
		r := rule.NewRule("ts_compile", "app")
		resolveImports(c, ix, r, imports, from)
		if r.Attr("deps") != nil {
			t.Errorf("%s imports: deps attribute was set to %v, want unset", name, r.AttrStrings("deps"))
		}
	}
}

func TestResolveImports_TsTestGetsRuntimeDeps(t *testing.T) {
	c := emptyConfig()
	tc := makeConfig("", []rule.Directive{
		directive("ts_runtime_dep", "@npm//:happy-dom"),
		directive("ts_runtime_dep", "@npm//:vitest_coverage-v8"),
	})
	c.Exts[languageName] = tc
	ix := buildIndex(t, c)
	from := label.New("", "src/lib", "math_test")

	tsTest := rule.NewRule("ts_test", "math_test")
	resolveImports(c, ix, tsTest, []string{"zod"}, from)
	got := tsTest.AttrStrings("deps")
	want := []string{"@npm//:happy-dom", "@npm//:vitest_coverage-v8", "@npm//:zod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ts_test deps = %v, want %v", got, want)
	}

	// The same runtime deps must NOT be added to a non-test rule.
	tsCompile := rule.NewRule("ts_compile", "math")
	resolveImports(c, ix, tsCompile, []string{"zod"}, from)
	if got, want := tsCompile.AttrStrings("deps"), []string{"@npm//:zod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_compile deps = %v, want %v", got, want)
	}
}

// ---- relative imports ------------------------------------------------------

func TestIsRelativeImport(t *testing.T) {
	for imp, want := range map[string]bool{
		"./utils":     true,
		"../lib":      true,
		"../../a/b":   true,
		"zod":         false,
		"@scope/pkg":  false,
		"@/utils":     false,
		"/abs/path":   false,
		"node:fs":     false,
		".hidden/pkg": false,
	} {
		if got := isRelativeImport(imp); got != want {
			t.Errorf("isRelativeImport(%q) = %v, want %v", imp, got, want)
		}
	}
}

func TestResolveRelative(t *testing.T) {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", nil)
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "src/lib", srcs: []string{"index.ts", "math.ts"}},
		indexedRule{kind: "ts_compile", name: "app", pkg: "src/app", srcs: []string{"index.ts", "main.ts"}},
		indexedRule{kind: "css_module", name: "styles", pkg: "src/app", srcs: []string{"Button.module.css"}},
		// A target whose name is not its directory basename: only an index hit
		// can produce this label, so a row wanting it cannot be satisfied by
		// the constructed-label fallback.
		indexedRule{kind: "ts_compile", name: "core", pkg: "src/lib", srcs: []string{"helper.ts"}},
	)
	from := label.New("", "src/app", "app")

	for _, tt := range []struct {
		name string
		imp  string
		want string
	}{
		{"file in sibling package", "../lib/math", "//src/lib"},
		{"directory with index.ts", "../lib", "//src/lib"},
		{"same package is not a dep", "./main", ""},
		// A hit inside the importing package is emitted as a package-relative
		// label, which is what Bazel wants in that BUILD file.
		{"css module by filename", "./Button.module.css", ":styles"},
		{"unindexed directory falls back to a constructed label", "../generated/api", "//src/generated/api"},
		// The extension a specifier spells out is not part of any index key:
		// importsForRule drops it from every source it indexes. These are the
		// two ways TypeScript lets one be spelled out.
		{"nodenext .js specifier for a .ts source", "../lib/helper.js", "//src/lib:core"},
		{"allowImportingTsExtensions specifier", "../lib/helper.ts", "//src/lib:core"},
		{"nodenext .js specifier inside the package", "./main.js", ""},
	} {
		if got := resolveRelative(c, ix, tt.imp, from); got != tt.want {
			t.Errorf("%s: resolveRelative(%q) = %q, want %q", tt.name, tt.imp, got, tt.want)
		}
	}
}

// A specifier reaching a target whose srcs live in a subdirectory of its own
// package -- the layout `# gazelle:exclude` protects -- resolves to that target
// and not to a package label for the subdirectory, which is not a package.
func TestResolveRelative_SrcsInSubdirectoryOfThePackage(t *testing.T) {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", nil)
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "everything", pkg: "pkg", srcs: []string{"nested/leaf.ts", "util.js", "root.ts"}},
	)
	from := label.New("", "consumer", "consumer")

	for imp, want := range map[string]string{
		"../pkg/nested/leaf.js": "//pkg:everything",
		"../pkg/nested/leaf":    "//pkg:everything",
		"../pkg/util.js":        "//pkg:everything",
	} {
		if got := resolveRelative(c, ix, imp, from); got != want {
			t.Errorf("resolveRelative(%q) = %q, want %q", imp, got, want)
		}
	}
}

func TestLabelForUnindexed(t *testing.T) {
	// The package is the module's directory. For a path naming a file that is
	// the parent -- naming the file itself would be a package that cannot
	// exist -- and for a directory import it is the path itself.
	from := label.New("", "src/app", "app")
	for rel, want := range map[string]string{
		"src/lib/math.ts":           "//src/lib",
		"src/lib/math.js":           "//src/lib",
		"src/lib/data.json":         "//src/lib",
		"src/lib/Button.module.css": "//src/lib",
		"src/lib/logo.svg":          "//src/lib",
		"src/lib/index.ts":          "//src/lib",
		"src/lib/index":             "//src/lib",
		"src/lib":                   "//src/lib",
		// A dot in a directory name is not an extension.
		"src/lib.v2": "//src/lib.v2",
		// Nothing outside the workspace, and nothing for the importer's own
		// package: a label on that would be a cycle.
		"index.ts":     "",
		"src/app/x.ts": "",
		"src/app":      "",
		"..":           "",
		"":             "",
	} {
		if got := labelForUnindexed(rel, from); got != want {
			t.Errorf("labelForUnindexed(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestModuleIndexKeys_DropsTheSpelledOutExtension(t *testing.T) {
	keys := moduleIndexKeys("src/lib/math.js", []string{".ts"})
	want := []string{
		"src/lib/math.js",
		"src/lib/math",
		"src/lib/math.ts",
		"src/lib/math/index.ts",
		"src/lib/math/index.tsx",
	}
	if len(keys) != len(want) {
		t.Fatalf("moduleIndexKeys = %q, want %q", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("moduleIndexKeys[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

// ---- path aliases ----------------------------------------------------------

func aliasConfig() *config.Config {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", []rule.Directive{
		directive("ts_path_alias", "@/ src/"),
		directive("ts_path_alias", "~ui/ packages/ui/src/"),
	})
	return c
}

func TestIsPathAlias(t *testing.T) {
	tc := getConfig(aliasConfig())
	for imp, want := range map[string]bool{
		"@/lib/math":     true,
		"~ui/Button":     true,
		"@types/node":    false,
		"@tanstack/form": false,
		"./relative":     false,
		"zod":            false,
	} {
		if got := isPathAlias(tc, imp); got != want {
			t.Errorf("isPathAlias(%q) = %v, want %v", imp, got, want)
		}
	}
}

func TestResolvePathAlias(t *testing.T) {
	c := aliasConfig()
	tc := getConfig(c)
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "src/lib", srcs: []string{"index.ts", "math.ts"}},
		// A barrel package: its index.ts is what makes "src/utils" itself an
		// import target, which is what the sub-path fallback below looks for.
		indexedRule{kind: "ts_compile", name: "utils", pkg: "src/utils", srcs: []string{"index.ts"}},
		indexedRule{kind: "ts_compile", name: "ui", pkg: "packages/ui/src", srcs: []string{"index.ts"}},
		indexedRule{kind: "ts_compile", name: "core", pkg: "src/lib", srcs: []string{"helper.ts"}},
	)
	from := label.New("", "src/app", "app")

	for _, tt := range []struct {
		name string
		imp  string
		want string
	}{
		{"alias to a file", "@/lib/math", "//src/lib"},
		{"alias to a barrel directory", "@/lib", "//src/lib"},
		{"second alias with a different prefix", "~ui/", "//packages/ui/src:ui"},
		{"sub-path compiled into the parent package", "@/utils/helpers", "//src/utils"},
		{"unindexed alias target falls back to a label", "@/generated/api", "//src/generated/api"},
		// An alias specifier can spell the extension out for the same reasons a
		// relative one can.
		{"alias with a nodenext .js extension", "@/lib/helper.js", "//src/lib:core"},
		{"alias with a .ts extension", "@/lib/helper.ts", "//src/lib:core"},
		{"unindexed alias file falls back to its directory", "@/generated/api.ts", "//src/generated"},
	} {
		if got := resolvePathAlias(c, ix, tc, tt.imp, from); got != tt.want {
			t.Errorf("%s: resolvePathAlias(%q) = %q, want %q", tt.name, tt.imp, got, tt.want)
		}
	}

	if got := resolvePathAlias(c, ix, tc, "zod", from); got != "" {
		t.Errorf("resolvePathAlias on a non-alias import = %q, want \"\"", got)
	}
}

func TestResolveImport_DispatchesOnSpecifierShape(t *testing.T) {
	c := aliasConfig()
	tc := getConfig(c)
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "lib", pkg: "src/lib", srcs: []string{"index.ts"}},
	)
	from := label.New("", "src/app", "app")

	for imp, want := range map[string]string{
		"../lib":     "//src/lib",
		"@/lib":      "//src/lib",
		"zod":        "@npm//:zod",
		"node:fs":    "",
		"@types/pkg": "@npm//:types_pkg",
	} {
		if got := resolveImport(c, ix, tc, imp, from); got != want {
			t.Errorf("resolveImport(%q) = %q, want %q", imp, got, want)
		}
	}
}

// ---- npm labels ------------------------------------------------------------

func TestResolveNpmPackage(t *testing.T) {
	tc := makeConfig("", nil)
	for imp, want := range map[string]string{
		"zod":                      "@npm//:zod",
		"vitest":                   "@npm//:vitest",
		"react/jsx-runtime":        "@npm//:react",
		"@types/react":             "@npm//:types_react",
		"@tanstack/router":         "@npm//:tanstack_router",
		"@tanstack/router/history": "@npm//:tanstack_router",
		"node:fs":                  "",
		"./relative":               "",
		"/absolute":                "",
		"@vitejs/plugin-react-swc": "@npm//:vitejs_plugin-react-swc",
		"lodash.debounce":          "@npm//:lodash.debounce",
		"@scope/pkg/deep/sub/path": "@npm//:scope_pkg",
		"unscoped/deep/sub/path":   "@npm//:unscoped",
	} {
		if got := resolveNpmPackage(tc, imp); got != want {
			t.Errorf("resolveNpmPackage(%q) = %q, want %q", imp, got, want)
		}
	}
}

func TestResolveNpmPackage_ExplicitMappingWins(t *testing.T) {
	tc := makeConfig("", nil)
	tc.npmPackages = map[string]string{
		"react":            "@npm_features//:react",
		"@tanstack/router": "//third_party/tanstack:router",
	}

	for imp, want := range map[string]string{
		"react":                    "@npm_features//:react",
		"react/jsx-runtime":        "@npm_features//:react",
		"@tanstack/router/history": "//third_party/tanstack:router",
		"zod":                      "@npm//:zod",
	} {
		if got := resolveNpmPackage(tc, imp); got != want {
			t.Errorf("resolveNpmPackage(%q) = %q, want %q", imp, got, want)
		}
	}
}

func TestNpmPackageToLabelName(t *testing.T) {
	for pkg, want := range map[string]string{
		"vitest":           "vitest",
		"@types/react":     "types_react",
		"@tanstack/router": "tanstack_router",
		"@scope/a-b.c":     "scope_a-b.c",
		"lodash.debounce":  "lodash.debounce",
	} {
		if got := npmPackageToLabelName(pkg); got != want {
			t.Errorf("npmPackageToLabelName(%q) = %q, want %q", pkg, got, want)
		}
	}
}

func TestBarePackageName(t *testing.T) {
	for imp, want := range map[string]string{
		"react":                    "react",
		"react/jsx-runtime":        "react",
		"@tanstack/router":         "@tanstack/router",
		"@tanstack/router/history": "@tanstack/router",
		"@scope":                   "@scope",
		"a/b/c":                    "a",
	} {
		if got := barePackageName(imp); got != want {
			t.Errorf("barePackageName(%q) = %q, want %q", imp, got, want)
		}
	}
}

// ---- extensions ------------------------------------------------------------
// isNodeBuiltin is covered by TestIsNodeBuiltin in imports_test.go.

func TestDropTsExtension(t *testing.T) {
	for name, want := range map[string]string{
		"math.ts":           "math",
		"Button.tsx":        "Button",
		"legacy.js":         "legacy",
		"styles.module.css": "styles.module.css",
		"config.json":       "config.json",
		"no-extension":      "no-extension",
		"a.ts.backup":       "a.ts.backup",
	} {
		if got := dropTsExtension(name); got != want {
			t.Errorf("dropTsExtension(%q) = %q, want %q", name, got, want)
		}
	}
}

// ---- path alias precedence -------------------------------------------------

// aliasPermutations returns every insertion order of the given entries as a
// distinct map, so that a test cannot pass by luck of one map layout.
func aliasPermutations(entries [][2]string) []map[string]string {
	if len(entries) == 0 {
		return []map[string]string{{}}
	}
	var out []map[string]string
	for i := range entries {
		rest := make([][2]string, 0, len(entries)-1)
		rest = append(rest, entries[:i]...)
		rest = append(rest, entries[i+1:]...)
		for _, tail := range aliasPermutations(rest) {
			m := map[string]string{entries[i][0]: entries[i][1]}
			for k, v := range tail {
				m[k] = v
			}
			out = append(out, m)
		}
	}
	return out
}

// overlappingAliases is an alias set in which several keys match the same
// specifier: the shape a tsconfig produces when it declares both "@shared" and
// "@shared/*".
var overlappingAliases = [][2]string{
	{"@", "src/root"},
	{"@/", "src/"},
	{"@shared", "src/shared/index"},
	{"@shared/", "src/shared/"},
	{"@shared/deep/", "src/shared/deep/"},
}

func TestMatchPathAlias_LongestKeyWinsUnderEveryMapOrder(t *testing.T) {
	perms := aliasPermutations(overlappingAliases)
	if len(perms) != 120 {
		t.Fatalf("expected 120 permutations of 5 entries, got %d", len(perms))
	}

	for _, tt := range []struct {
		name       string
		imp        string
		wantOK     bool
		wantPrefix string
		wantDir    string
		wantRest   string
	}{
		{"wildcard key beats the shorter whole-module key", "@shared/value", true, "@shared/", "src/shared/", "value"},
		{"the whole-module key matches the bare specifier", "@shared", true, "@shared", "src/shared/index", ""},
		{"deepest wildcard key wins", "@shared/deep/leaf", true, "@shared/deep/", "src/shared/deep/", "leaf"},
		{"a key only matches at a segment boundary", "@sharedX/value", false, "", "", ""},
		{"single-character key still matches its own subtree", "@/lib/math", true, "@/", "src/", "lib/math"},
		{"bare key matches itself exactly", "@", true, "@", "src/root", ""},
		{"a bare specifier is not an alias", "react", false, "", "", ""},
	} {
		for i, aliases := range perms {
			tc := &tsConfig{pathAliases: aliases}
			got, ok := matchPathAlias(tc, tt.imp)
			if ok != tt.wantOK {
				t.Fatalf("%s (perm %d): matchPathAlias(%q) ok = %v, want %v", tt.name, i, tt.imp, ok, tt.wantOK)
			}
			if !tt.wantOK {
				continue
			}
			if got.prefix != tt.wantPrefix || got.dir != tt.wantDir || got.rest != tt.wantRest {
				t.Fatalf("%s (perm %d): matchPathAlias(%q) = {prefix:%q dir:%q rest:%q}, want {prefix:%q dir:%q rest:%q}",
					tt.name, i, tt.imp, got.prefix, got.dir, got.rest, tt.wantPrefix, tt.wantDir, tt.wantRest)
			}
		}
	}
}

func TestAliasRest(t *testing.T) {
	for _, tt := range []struct {
		prefix, imp, wantRest string
		wantOK                bool
	}{
		{"@/", "@/lib", "lib", true},
		{"@/", "@/", "", true},
		{"@/", "@x", "", false},
		{"@shared", "@shared", "", true},
		{"@shared", "@shared/value", "value", true},
		{"@shared", "@shared/deep/leaf", "deep/leaf", true},
		{"@shared", "@sharedX", "", false},
		{"@shared", "@share", "", false},
		{"packages/shared", "packages/shared-ui/x", "", false},
	} {
		gotRest, gotOK := aliasRest(tt.prefix, tt.imp)
		if gotOK != tt.wantOK || gotRest != tt.wantRest {
			t.Errorf("aliasRest(%q, %q) = (%q, %v), want (%q, %v)",
				tt.prefix, tt.imp, gotRest, gotOK, tt.wantRest, tt.wantOK)
		}
	}
}

// TestResolvePathAlias_OverlappingKeysResolveToOneLabel pins the generated dep
// for a specifier that several alias keys match. Before longest-key precedence
// this returned either //src/shared or //src/shared/index/value depending on
// map iteration order, and a hard strict-deps error turns the wrong one into a
// failing build.
func TestResolvePathAlias_OverlappingKeysResolveToOneLabel(t *testing.T) {
	c := emptyConfig()
	// No index.ts: a barrel would make both candidate expansions converge on
	// the same label and hide the defect.
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_compile", name: "shared", pkg: "src/shared", srcs: []string{"value.ts"}},
	)
	from := label.New("", "src/app", "app")

	for _, tt := range []struct {
		imp  string
		want string
	}{
		{"@shared/value", "//src/shared"},
		{"@shared", "//src/shared"},
		{"@sharedX/value", ""},
	} {
		for i, aliases := range aliasPermutations(overlappingAliases) {
			tc := &tsConfig{pathAliases: aliases}
			if got := resolvePathAlias(c, ix, tc, tt.imp, from); got != tt.want {
				t.Fatalf("perm %d: resolvePathAlias(%q) = %q, want %q", i, tt.imp, got, tt.want)
			}
		}
	}
}

func TestIsPathAlias_UsesTheSameMatcherAsResolution(t *testing.T) {
	tc := &tsConfig{pathAliases: map[string]string{
		"@shared":  "src/shared/index",
		"@shared/": "src/shared/",
	}}
	for imp, want := range map[string]bool{
		"@shared":        true,
		"@shared/value":  true,
		"@sharedX":       false,
		"@sharedX/value": false,
		"react":          false,
	} {
		if got := isPathAlias(tc, imp); got != want {
			t.Errorf("isPathAlias(%q) = %v, want %v", imp, got, want)
		}
	}
}

func TestNpmLabelForImport_RejectsSchemeSpecifiers(t *testing.T) {
	tc := &tsConfig{}
	for _, imp := range []string{"virtual:answer", "virtual:routes/generated", "data:text/javascript,0"} {
		if got := resolveNpmPackage(tc, imp); got != "" {
			t.Errorf("resolveNpmPackage(%q) = %q, want \"\" (a target name cannot contain ':')", imp, got)
		}
	}
	if got := resolveNpmPackage(tc, "zod"); got != "@npm//:zod" {
		t.Errorf("resolveNpmPackage(\"zod\") = %q, want @npm//:zod", got)
	}
}

// ---- bare specifiers -------------------------------------------------------

func TestResolveImport_BareSpecifiers(t *testing.T) {
	c := emptyConfig()
	c.Exts[languageName] = makeConfig("", nil)
	tc := getConfig(c)
	ix := buildIndex(t, c,
		indexedRule{
			kind: "ts_compile", name: "lib", pkg: "packages/lib",
			srcs: []string{"index.ts"}, moduleName: "@acme/lib",
		},
	)
	from := label.New("", "app", "app")

	for _, tt := range []struct {
		name string
		imp  string
		want string
	}{
		// A workspace link's package name is a first-party target, not a
		// package in the hub.
		{"module_name of a first-party target", "@acme/lib", "//packages/lib"},
		// Node builtins are not packages under either spelling.
		{"bare builtin", "path", ""},
		{"bare builtin sub-path", "fs/promises", ""},
		{"prefixed builtin", "node:path", ""},
		// Everything else still gets the hub label.
		{"npm package", "zod", "@npm//:zod"},
		{"scoped npm package", "@tanstack/router", "@npm//:tanstack_router"},
	} {
		if got := resolveImport(c, ix, tc, tt.imp, from); got != tt.want {
			t.Errorf("%s: resolveImport(%q) = %q, want %q", tt.name, tt.imp, got, tt.want)
		}
	}
}

// A workspace with more than one npm hub is the normal case, not an exotic one:
// a curated fixture lockfile beside a real one, or a tool's dependencies kept
// out of an app's closure. The hub is a property of the package doing the
// importing, so it comes from a directive rather than from the repo.
func TestResolveNpmPackage_HubFromDirective(t *testing.T) {
	tc := makeConfig("", nil)
	tc.npmHub = "@npm_eslint"
	for imp, want := range map[string]string{
		"eslint":                    "@npm_eslint//:eslint",
		"@typescript-eslint/utils":  "@npm_eslint//:typescript-eslint_utils",
		"@typescript-eslint/parser": "@npm_eslint//:typescript-eslint_parser",
		// Still not packages, whichever hub is named.
		"node:fs":    "",
		"./relative": "",
	} {
		if got := resolveNpmPackage(tc, imp); got != want {
			t.Errorf("resolveNpmPackage(%q) = %q, want %q", imp, got, want)
		}
	}

	// An unset hub is the default, not "//:eslint" -- which would resolve to a
	// target in the importing repo rather than failing.
	tc.npmHub = ""
	if got := resolveNpmPackage(tc, "eslint"); got != "@npm//:eslint" {
		t.Errorf("an empty hub gave %q, want the @npm default", got)
	}
}

// Both spellings reach a BUILD file, so both have to mean the same hub.
func TestNormalizeNpmHub(t *testing.T) {
	for value, want := range map[string]string{
		"npm_eslint":     "@npm_eslint",
		"@npm_eslint":    "@npm_eslint",
		"@npm_eslint//":  "@npm_eslint",
		"  npm_eslint  ": "@npm_eslint",
		"":               "@npm",
	} {
		if got := normalizeNpmHub(value); got != want {
			t.Errorf("normalizeNpmHub(%q) = %q, want %q", value, got, want)
		}
	}
}
