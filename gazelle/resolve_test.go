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
	kind string
	name string
	pkg  string
	srcs []string
}

func newRule(ir indexedRule) (*rule.Rule, *rule.File) {
	r := rule.NewRule(ir.kind, ir.name)
	if ir.srcs != nil {
		r.SetAttr("srcs", ir.srcs)
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
	} {
		if got := resolveRelative(c, ix, tt.imp, from); got != tt.want {
			t.Errorf("%s: resolveRelative(%q) = %q, want %q", tt.name, tt.imp, got, tt.want)
		}
	}
}

func TestLabelFromRel(t *testing.T) {
	// The fallback treats the whole path as a package whose target is named
	// after its last segment -- which is right for the directory imports it
	// exists for ("src/lib" -> //src/lib) and, for a file path, produces a
	// label for a package that does not exist ("src/lib/math.ts" ->
	// //src/lib/math). Pinned here as the current contract.
	for rel, want := range map[string]string{
		"src/lib/math.ts":  "//src/lib/math",
		"src/lib/index.ts": "//src/lib",
		"src/lib/index":    "//src/lib",
		"src/lib":          "//src/lib",
		"index.ts":         "",
		"":                 "",
	} {
		if got := labelFromRel(rel); got != want {
			t.Errorf("labelFromRel(%q) = %q, want %q", rel, got, want)
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
