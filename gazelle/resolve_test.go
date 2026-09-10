package typescript

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// ---- helpers ---------------------------------------------------------------

// indexedRule is one rule to put in the RuleIndex: a kind, a target name, the
// Bazel package it lives in, and its srcs.
type indexedRule struct {
	kind   string
	name   string
	pkg    string
	srcs   []string
	outDir string
	outs   []string
}

func newRule(ir indexedRule) (*rule.Rule, *rule.File) {
	r := rule.NewRule(ir.kind, ir.name)
	if ir.srcs != nil {
		r.SetAttr("srcs", ir.srcs)
	}
	if ir.outDir != "" {
		r.SetAttr("out_dir", ir.outDir)
	}
	if ir.outs != nil {
		r.SetAttr("outs", ir.outs)
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

// repoWithDirs roots c at a real tree holding dirs, for the rows whose expected

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
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

// Every src by its repository path, the key an edge target is looked up by.
func TestImportsForRule_TsCompile(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_compile", name: "components", pkg: "src/components",
		srcs: []string{"index.ts", "Button.tsx", "helpers.ts", "logo.svg"},
	})

	got := specStrings(importsForRule(c, r, f))
	want := []string{
		"src/components/index.ts",
		"src/components/Button.tsx",
		"src/components/helpers.ts",
		"src/components/logo.svg",
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
	want := []string{"src/lib/math.test.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importsForRule(ts_test) = %v, want %v", got, want)
	}
}

// A ":" pins a file whose name opens a label (labels.go); a ":" naming a rule
// in the same file is that rule, and a label into another package is no file.
func TestImportsForRule_LabelsInSrcsAreNotFiles(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_compile", name: "routes", pkg: "web/routes",
		srcs: []string{":@{$username}.tsx", ":gen", "//other:thing", "@npm//:x"},
	})
	f.Rules = append(f.Rules, rule.NewRule("ts_codegen", "gen"))

	got := specStrings(importsForRule(c, r, f))
	want := []string{"web/routes/@{$username}.tsx"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importsForRule = %v, want %v", got, want)
	}
}

func TestImportsForRule_UnknownKindIsNotImportable(t *testing.T) {
	c := emptyConfig()
	for _, kind := range []string{"genrule", "sh_binary", "filegroup"} {
		r, f := newRule(indexedRule{
			kind: kind, name: "thing", pkg: "src/app", srcs: []string{"index.ts"},
		})
		if got := importsForRule(c, r, f); got != nil {
			t.Errorf("importsForRule(%s) = %v, want nil (kind must not be indexed)",
				kind, got)
		}
	}
}

// ---- ts_codegen out_dir trees ----------------------------------------------

func TestImportsForRule_CodegenOutDirIsIndexed(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_codegen", name: "paraglide_messages", pkg: "web",
		srcs: []string{"i18n/settings.json"}, outDir: "shared/i18n/compiled",
	})

	got := specStrings(importsForRule(c, r, f))
	want := []string{
		"web/shared/i18n/compiled",
		"ts_codegen_tree:web/shared/i18n/compiled",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importsForRule(ts_codegen) = %v, want %v", got, want)
	}
}

func TestImportsForRule_CodegenOutsIsNotImportable(t *testing.T) {
	c := emptyConfig()
	r, f := newRule(indexedRule{
		kind: "ts_codegen", name: "api_types", pkg: "web",
		srcs: []string{"openapi.yaml"}, outs: []string{"api-types.ts"},
	})

	if got := importsForRule(c, r, f); got != nil {
		t.Errorf("importsForRule(outs ts_codegen) = %v, want nil: its outs are the companion ts_compile's modules, so no import resolves to the codegen", got)
	}
}

// Deepest root wins, so a tree generated inside another tree's directory
// answers for its own subtree instead of the outer one swallowing it.
func TestResolveCodegenTree_NearestRootWins(t *testing.T) {
	c := emptyConfig()
	ix := buildIndex(t, c,
		indexedRule{
			kind: "ts_codegen", name: "outer", pkg: "web",
			srcs: []string{"a.json"}, outDir: "gen",
		},
		indexedRule{
			kind: "ts_codegen", name: "inner", pkg: "web",
			srcs: []string{"b.json"}, outDir: "gen/inner",
		},
	)
	from := label.New("", "src/app", "app")

	for imp, want := range map[string]string{
		"web/gen/thing":       "//web:outer",
		"web/gen/inner/thing": "//web:inner",
		"web/other/thing":     "",
	} {
		if got := resolveCodegenTree(ix, imp, from); got != want {
			t.Errorf("resolveCodegenTree(%q) = %q, want %q", imp, got, want)
		}
	}
}

// The walk climbs a namespaced key, so a ts_compile indexing the very path a
// specifier's parent names can never be reached by prefix.
func TestResolveCodegenTree_OnlyReachesCodegenTrees(t *testing.T) {
	c := emptyConfig()
	ix := buildIndex(t, c, indexedRule{
		kind: "ts_compile", name: "utils", pkg: "src/utils", srcs: []string{"index.ts"},
	})
	from := label.New("", "src/app", "app")

	if got := resolveCodegenTree(ix, "src/utils/missing", from); got != "" {
		t.Errorf("resolveCodegenTree reached a ts_compile: %q, want \"\"", got)
	}
}

// A relative specifier that escapes the workspace root must not walk past it.
func TestResolveCodegenTree_StopsAtTheRoot(t *testing.T) {
	c := emptyConfig()
	ix := buildIndex(t, c)
	from := label.New("", "src/app", "app")

	for _, imp := range []string{"", ".", "..", "../thing", "/abs/thing", "thing"} {
		if got := resolveCodegenTree(ix, imp, from); got != "" {
			t.Errorf("resolveCodegenTree(%q) = %q, want \"\"", imp, got)
		}
	}
}

// ---- the listing as the resolver -------------------------------------------

const (
	storeJsxRuntime = store + "@types/react/19.0.0/aaa/node_modules/" +
		"@types/react/jsx-runtime.d.ts"
	storeTypesNode = store + "@types/node/22.20.1/bbb/node_modules/" +
		"@types/node/index.d.ts"
	storeTypescript = store + "@/typescript/5.9.2/kkk/node_modules/" +
		"typescript/lib/typescript.d.ts"
	storeVite = store + "@/vite/8.2.2/ccc/node_modules/vite/dist/node/" +
		"index.d.ts"
)

func viaLine(spec, from, packageID string) string {
	line := `   Imported via "` + spec + `" from file '` + from + `'`
	if packageID != "" {
		line += ` with packageId '` + packageID + `'`
	}
	return line + "\n"
}

func includeLine(pkg string) string {
	return "   Matched by include pattern 'src/**/*' in '" + pkg +
		"/tsconfig.json'\n"
}

// web's program: npm edges under every reason form, a member by name and by
// path, a self edge, two unowned JSON files and the toolchain's lib.
var webListing = storeZod + "\n" +
	viaLine("zod", "web/src/a.ts", "zod/index.d.ts@3.24.2") +
	storeMarked + "\n" +
	viaLine("marked", "web/src/a.ts", "marked/lib/marked.d.ts@15.0.12") +
	storeViteClient + "\n" +
	"   Type library referenced via 'vite/client' from file " +
	"'web/src/vite-env.d.ts' with packageId 'vite/client.d.ts@8.2.2'\n" +
	storeJsxRuntime + "\n" +
	"   Imported via \"react/jsx-runtime\" from file 'web/src/App.tsx' with " +
	"packageId '@types/react/jsx-runtime.d.ts@19.0.0' to import 'jsx' and " +
	"'jsxs' factory functions\n" +
	storeTypescript + "\n" +
	viaLine("typescript", "web/src/a.test.ts",
		"typescript/lib/typescript.d.ts@5.9.2") +
	storeTypesNode + "\n" +
	"   Entry point of type library 'node' specified in compilerOptions with " +
	"packageId '@types/node/index.d.ts@22.20.1'\n" +
	libES5 + "\n" +
	"   Default library for target 'ES2022'\n" +
	"packages/ui/src/index.ts\n" +
	viaLine("@acme/ui", "web/src/a.ts", "@acme/ui-src/src/index.ts@0.0.0") +
	"packages/ui/src/util.ts\n" +
	viaLine("../../packages/ui/src/util", "web/src/a.ts", "") +
	"go/fixtures/x.json\n" +
	viaLine("../../go/fixtures/x.json", "web/src/a.ts", "") +
	"packages/figma/manifest.json\n" +
	viaLine("../../packages/figma/manifest.json", "web/src/a.ts", "") +
	"web/src/a.ts\n" + includeLine("web") +
	viaLine("./a", "web/src/a.test.ts", "") +
	"web/src/b.ts\n" + includeLine("web") +
	viaLine("./b", "web/src/a.ts", "") +
	"web/src/App.tsx\n" + includeLine("web") +
	"web/src/vite-env.d.ts\n" + includeLine("web") +
	"web/src/a.test.ts\n" + includeLine("web")

// packages/lib's program: the member's own name through an exports subpath,
// from a library file and from a test file (the canvas-sdk shape).
var libListing = "packages/lib/src/index.ts\n" + includeLine("packages/lib") +
	"packages/lib/src/wire/index.ts\n" + includeLine("packages/lib") +
	viaLine("@acme/lib/wire", "packages/lib/src/index.ts",
		"@acme/lib/src/wire/index.ts@0.0.0") +
	viaLine("@acme/lib/wire", "packages/lib/src/x.test.ts",
		"@acme/lib/src/wire/index.ts@0.0.0") +
	"packages/lib/src/x.test.ts\n" + includeLine("packages/lib")

var edgeListings = map[string]string{
	"web":          webListing,
	"packages/lib": libListing,
	"packages/ui": listingOf("packages/ui", "packages/ui/src/index.ts",
		"packages/ui/src/util.ts"),
	"packages/figma": listingOf("packages/figma", "packages/figma/src/code.ts"),
}

// edgeRepo is a run from the root over npmRepo's workspace -- its lockfile and
// manifests, an install, web/package.json's dependencies -- with the listings.
func edgeRepo(t *testing.T, listings map[string]string,
) (*config.Config, *tsConfig) {
	t.Helper()
	root, _ := npmRepo(t)
	writeFile(t, filepath.Join(root, "web/package.json"), `{"name": "web-app",
	  "dependencies": {"marked": "^15.0.0"},
	  "devDependencies": {"@types/react": "^19.0.0", "typescript": "5.9.2"}}`)
	writeFile(t, filepath.Join(root, "node_modules/.modules.yaml"),
		"layoutVersion: 5\n")
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	(&resolve.Configurer{}).RegisterFlags(nil, "", c)
	configureTsConfig(c, "", nil)
	tc := getConfig(c)
	tc.programs.visit("", nil)
	for dir, text := range listings {
		p := programOf(t, dir, text)
		for _, f := range p.Files {
			if !firstParty(f) {
				continue
			}
			for d := parentDir(f); d != ""; d = parentDir(d) {
				tc.programs.visit(d, nil)
			}
		}
		tc.programs.record(p)
	}
	return c, tc
}

// The rules a run over edgeListings writes, as the index sees them.
var edgeRules = []indexedRule{
	{kind: "ts_compile", name: "web", pkg: "web",
		srcs: []string{"src/App.tsx", "src/a.ts", "src/b.ts", "src/vite-env.d.ts"}},
	{kind: "ts_test", name: "web_test", pkg: "web",
		srcs: []string{"src/a.test.ts", "src/vite-env.d.ts"}},
	{kind: "ts_compile", name: "ui", pkg: "packages/ui",
		srcs: []string{"src/index.ts", "src/util.ts"}},
	{kind: "ts_compile", name: "lib", pkg: "packages/lib",
		srcs: []string{"src/index.ts", "src/wire/index.ts"}},
	{kind: "ts_test", name: "lib_test", pkg: "packages/lib",
		srcs: []string{"src/x.test.ts"}},
	{kind: "ts_compile", name: "figma", pkg: "packages/figma",
		srcs: []string{"src/code.ts"}},
}

func resolveEdgesOf(t *testing.T, c *config.Config, ix *resolve.RuleIndex,
	kind, pkg, name string, imps *ruleImports) (*rule.Rule, string) {
	t.Helper()
	r := rule.NewRule(kind, name)
	from := label.New("", pkg, name)
	logged := captureLog(t, func() { resolveEdges(c, ix, r, imps, from) })
	return r, logged
}

// One label per edge target, by where it sits and who owns it; the test file's
// edge is not the ts_compile's, and no types attribute is written.
func TestResolveEdges_CompileDepsFromTheListing(t *testing.T) {
	c, tc := edgeRepo(t, edgeListings)
	ix := buildIndex(t, c, edgeRules...)
	s := tc.programs

	imps := s.compileImports("web", s.srcs("web", tc))
	for _, e := range imps.edges {
		if e.From == "web/src/a.test.ts" {
			t.Errorf("compileImports carries the test file's edge %+v", e)
		}
	}
	r, logged := resolveEdgesOf(t, c, ix, "ts_compile", "web", "web", imps)
	want := []string{
		"//packages/ui",
		":node_modules/@acme/ui",
		"@npm//:types_node",
		"@npm//:vite",
		"@npm//:zod",
		"@npm//web:marked",
		"@npm//web:react",
	}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %q, want %q", got, want)
	}
	if r.Attr("types") != nil {
		t.Errorf("types = %v, want none: the tsconfig owns it", r.Attr("types"))
	}
	for _, want := range []string{
		"web/src/a.ts imports go/fixtures/x.json: no package owns it: " +
			"no tsconfig.json above it lists a file; no dep",
		"web/src/a.ts imports packages/figma/manifest.json: no package " +
			"owns it: packages/figma/tsconfig.json, the nearest, does not " +
			"list it; no dep",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log lacks %q:\n%s", want, logged)
		}
	}
	if n := strings.Count(logged, "\n"); n != 2 {
		t.Errorf("%d log lines, want the two unowned reports:\n%s", n, logged)
	}
}

// A src the holding package's program does not list -- a regular file under
// it, by the walk -- is that rule's dep: the index answers before ownership.
func TestResolveEdges_DepOnAnUnlistedSrc(t *testing.T) {
	c, tc := edgeRepo(t, edgeListings)
	rules := append([]indexedRule{}, edgeRules...)
	for i, ir := range rules {
		if ir.pkg == "packages/figma" {
			rules[i].srcs = []string{"manifest.json", "src/code.ts"}
		}
	}
	ix := buildIndex(t, c, rules...)
	s := tc.programs
	r, logged := resolveEdgesOf(t, c, ix, "ts_compile", "web", "web",
		s.compileImports("web", s.srcs("web", tc)))
	want := []string{
		"//packages/figma",
		"//packages/ui",
		":node_modules/@acme/ui",
		"@npm//:types_node",
		"@npm//:vite",
		"@npm//:zod",
		"@npm//web:marked",
		"@npm//web:react",
	}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %q, want %q", got, want)
	}
	if strings.Contains(logged, "manifest.json") ||
		strings.Count(logged, "\n") != 1 {
		t.Errorf("log, want x.json's report alone:\n%s", logged)
	}
	// A file a rule of the importing package holds is nothing, whichever rule.
	r, logged = resolveEdgesOf(t, c, ix, "ts_compile", "web", "web",
		&ruleImports{edges: []explainfiles.Edge{
			importEdge("web/src/b.ts", "./a.test", "web/src/a.test.ts")}})
	if r.Attr("deps") != nil || logged != "" {
		t.Errorf("deps = %v, log %q; want none", r.Attr("deps"), logged)
	}
}

// ts_test.deps: the ts_compile, every owned file's edges, the vitest config's
// and the manifest union in D7 spelling, with no label said twice.
func TestResolveEdges_TestDepsCarryTheRuntime(t *testing.T) {
	c, tc := edgeRepo(t, edgeListings)
	ix := buildIndex(t, c, edgeRules...)
	s := tc.programs
	const cfg = "web/vitest.config.mts"
	s.vitestEdges = map[string][]explainfiles.Edge{
		cfg: {importEdge(cfg, "vite", storeVite)}}

	set := s.srcs("web", tc)
	imps := s.testImports(c.RepoRoot, tc.lock, "web", ":web", cfg, set)
	if imps.config != cfg {
		t.Errorf("config = %q, want %q", imps.config, cfg)
	}
	if !s.vitestConfigs[cfg] {
		t.Errorf("%s is not registered for the combined run", cfg)
	}
	r, logged := resolveEdgesOf(t, c, ix, "ts_test", "web", "web_test", imps)
	want := []string{
		"//packages/ui",
		":node_modules/@acme/ui",
		":web",
		"@npm//:types_node",
		"@npm//:typescript",
		"@npm//:vite",
		"@npm//:zod",
		"@npm//web:marked",
		"@npm//web:react",
		"@npm//web:types_react",
	}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %q, want %q", got, want)
	}
	if n := strings.Count(logged, "\n"); n != 2 {
		t.Errorf("%d log lines, want the two unowned reports:\n%s", n, logged)
	}
	if r.Attr("config_srcs") != nil {
		t.Errorf("config_srcs = %q, want unset: the config imports no module "+
			"of its own", r.AttrStrings("config_srcs"))
	}
}

// ts_test.config_srcs: the modules the config's closure reaches, spelled from
// the test's package; one outside the config's package is said and gets none.
func TestResolveEdges_ConfigSrcsAreTheConfigsModules(t *testing.T) {
	c, tc := edgeRepo(t, edgeListings)
	ix := buildIndex(t, c, edgeRules...)
	s := tc.programs
	const (
		cfg     = "web/vitest.config.mts"
		plugin  = "web/plugins/define.ts"
		meta    = "web/plugins/meta.json"
		outside = "shared/vitest.base.ts"
	)
	s.vitestEdges = map[string][]explainfiles.Edge{
		cfg: {
			importEdge(cfg, "./plugins/define", plugin),
			importEdge(cfg, "../shared/vitest.base", outside),
		},
		plugin: {
			importEdge(plugin, "./meta.json", meta),
			importEdge(plugin, "zod", storeZod),
		},
		outside: {importEdge(outside, "vite", storeVite)},
	}
	set := s.srcs("web", tc)
	imps := s.testImports(c.RepoRoot, tc.lock, "web", ":web", cfg, set)
	r, logged := resolveEdgesOf(t, c, ix, "ts_test", "web", "web_test", imps)
	want := []string{"plugins/define.ts", "plugins/meta.json"}
	if got := r.AttrStrings("config_srcs"); !reflect.DeepEqual(got, want) {
		t.Errorf("config_srcs = %q, want %q", got, want)
	}
	for _, dep := range []string{"@npm//:zod", "@npm//:vite"} {
		if !hasLabel(r.AttrStrings("deps"), dep) {
			t.Errorf("deps %q lack %s, the closure's npm edge",
				r.AttrStrings("deps"), dep)
		}
	}
	if !strings.Contains(logged, outside) ||
		!strings.Contains(logged, "config_srcs") {
		t.Errorf("log, want %s said as outside the config's package:\n%s",
			outside, logged)
	}

	// The same config from a test in a package below: the config's package's.
	r, _ = resolveEdgesOf(t, c, ix, "ts_test", "web/test", "test_test",
		&ruleImports{config: cfg})
	want = []string{"//web:plugins/define.ts", "//web:plugins/meta.json"}
	if got := r.AttrStrings("config_srcs"); !reflect.DeepEqual(got, want) {
		t.Errorf("config_srcs from web/test = %q, want %q", got, want)
	}
}

// A self-import through an exports subpath: the own view from a ts_test, whose
// runtime resolves the name through node_modules; nothing from the ts_compile.
func TestResolveEdges_MemberSelfImport(t *testing.T) {
	c, tc := edgeRepo(t, edgeListings)
	ix := buildIndex(t, c, edgeRules...)
	s := tc.programs
	set := s.srcs("packages/lib", tc)

	r, logged := resolveEdgesOf(t, c, ix, "ts_compile", "packages/lib", "lib",
		s.compileImports("packages/lib", set))
	if r.Attr("deps") != nil || logged != "" {
		t.Errorf("ts_compile deps = %v, log %q; want none", r.Attr("deps"), logged)
	}
	r, _ = resolveEdgesOf(t, c, ix, "ts_test", "packages/lib", "lib_test",
		s.testImports(c.RepoRoot, tc.lock, "packages/lib", ":lib", "", set))
	want := []string{"//:node_modules/@acme/lib", ":lib"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("ts_test deps = %q, want %q", got, want)
	}
}

// A file under a declared out_dir is that codegen's, whatever the program
// listed; a types entry naming an absent codegen out is a dep on it (D9).
func TestResolveEdges_CodegenOutputs(t *testing.T) {
	const gen = "worker/gen/types.d.ts"
	c, tc := edgeRepo(t, map[string]string{
		"worker": listingOf("worker", "worker/src/index.ts") + gen + "\n" +
			viaLine("../gen/types", "worker/src/index.ts", ""),
		"worker2": listingOf("worker2", "worker2/src/index.ts"),
	})
	writeFile(t, filepath.Join(c.RepoRoot, "worker2/tsconfig.json"),
		`{"compilerOptions": {"types": ["./worker-configuration.d.ts"]}}`)
	ix := buildIndex(t, c,
		indexedRule{kind: "ts_codegen", name: "tree", pkg: "worker", outDir: "gen"},
		indexedRule{kind: "ts_compile", name: "worker", pkg: "worker",
			srcs: []string{"src/index.ts"}},
		indexedRule{kind: "ts_compile", name: "worker2", pkg: "worker2",
			srcs: []string{"src/index.ts"}},
	)
	s := tc.programs

	f, err := rule.LoadData("BUILD.bazel", "worker",
		[]byte(`ts_codegen(name = "tree", out_dir = "gen")`))
	if err != nil {
		t.Fatal(err)
	}
	configureTsConfig(c, "worker", f)
	set := s.srcs("worker", getConfig(c))
	if len(set.declaration) != 0 {
		t.Errorf("srcs(worker) = %+v: the out_dir's file is no src", set)
	}
	r, logged := resolveEdgesOf(t, c, ix, "ts_compile", "worker", "worker",
		s.compileImports("worker", set))
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, []string{":tree"}) {
		t.Errorf("worker deps = %q, log %q; want [:tree]", got, logged)
	}

	c.Exts[languageName] = tc
	f, err = rule.LoadData("BUILD.bazel", "worker2",
		[]byte(`ts_codegen(name = "wt", outs = ["worker-configuration.d.ts"])`))
	if err != nil {
		t.Fatal(err)
	}
	configureTsConfig(c, "worker2", f)
	r, logged = resolveEdgesOf(t, c, ix, "ts_compile", "worker2", "worker2",
		s.compileImports("worker2", s.srcs("worker2", getConfig(c))))
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, []string{":wt"}) {
		t.Errorf("worker2 deps = %q, log %q; want [:wt]", got, logged)
	}
}

// Core # gazelle:resolve names the label for a file no rule indexes; the
// override is read before ownership, so the report goes too.
func TestResolveEdges_OverrideIsTheEscapeHatch(t *testing.T) {
	c, tc := edgeRepo(t, edgeListings)
	ix := buildIndex(t, c, edgeRules...)
	f, err := rule.LoadData("BUILD.bazel", "", []byte(
		"# gazelle:resolve typescript go/fixtures/x.json //go/fixtures:json\n"))
	if err != nil {
		t.Fatal(err)
	}
	(&resolve.Configurer{}).Configure(c, "", f)
	s := tc.programs

	r, logged := resolveEdgesOf(t, c, ix, "ts_compile", "web", "web",
		s.compileImports("web", s.srcs("web", tc)))
	if deps := r.AttrStrings("deps"); !hasLabel(deps, "//go/fixtures:json") {
		t.Errorf("deps = %q, want //go/fixtures:json among them", deps)
	}
	if strings.Contains(logged, "go/fixtures/x.json") {
		t.Errorf("the overridden file was still reported:\n%s", logged)
	}
}

// Without a lockfile there is no hub: an npm edge gets no label, said once.
func TestResolveEdges_NoLockfileNoNpmLabel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "w"}`)
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	(&resolve.Configurer{}).RegisterFlags(nil, "", c)
	configureTsConfig(c, "", nil)
	tc := getConfig(c)
	s := tc.programs
	s.visit("", nil)
	s.visit("app", nil)
	s.record(programOf(t, "app", listingOf("app", "app/a.ts", "app/b.ts")+
		storeZod+"\n"+viaLine("zod", "app/a.ts", "")+viaLine("zod", "app/b.ts", "")))
	ix := buildIndex(t, c, indexedRule{kind: "ts_compile", name: "app",
		pkg: "app", srcs: []string{"a.ts", "b.ts"}})

	r, logged := resolveEdgesOf(t, c, ix, "ts_compile", "app", "app",
		s.compileImports("app", s.srcs("app", tc)))
	if r.Attr("deps") != nil {
		t.Errorf("deps = %v, want none", r.Attr("deps"))
	}
	if n := strings.Count(logged, pnpmLockfileName); n != 1 {
		t.Errorf("the missing lockfile was said %d times, want once:\n%s", n, logged)
	}
}

// The exact repository path of every src is what an edge target is looked up
// by; the module-form keys beside it are the specifier ladder's.
func TestImportsForRule_IndexesTheExactPath(t *testing.T) {
	r, f := newRule(indexedRule{kind: "ts_compile", name: "w", pkg: "w",
		srcs: []string{"src/a.ts", "src/types.d.ts", "data.json", "m.d.mts"}})
	got := specStrings(importsForRule(nil, r, f))
	for _, want := range []string{"w/src/a.ts", "w/src/types.d.ts",
		"w/data.json", "w/m.d.mts"} {
		if !contains(got, want) {
			t.Errorf("specs %q lack the exact path %q", got, want)
		}
	}
	if n := strings.Count(strings.Join(got, "\n")+"\n", "w/data.json\n"); n != 1 {
		t.Errorf("w/data.json indexed %d times, want once", n)
	}
}

// ---- the Workers pool -------------------------------------------------------

// worker/ exports a pool config naming ./wrangler.jsonc and declares the pool;
// worker/test is a package of its own; lock says where istanbul is declared.
func poolRepo(t *testing.T, lock string) (*config.Config, *tsConfig) {
	t.Helper()
	root := t.TempDir()
	writeWorkspace(t, root, map[string]string{
		pnpmLockfileName:             lock,
		"node_modules/.modules.yaml": "layoutVersion: 5\n",
		"package.json":               rootManifest,
		"worker/package.json": `{"name":"worker","devDependencies":` +
			`{"@cloudflare/vitest-pool-workers":"0.18.4","vitest":"4.1.11"}}` +
			"\n",
		"worker/vitest.config.mts": poolConfig("./wrangler.jsonc"),
		"worker/wrangler.jsonc":    `{"main":"src/index.ts"}` + "\n",
	})
	c := &config.Config{RepoRoot: root, Exts: make(map[string]interface{})}
	(&resolve.Configurer{}).RegisterFlags(nil, "", c)
	configureTsConfig(c, "", nil)
	tc := getConfig(c)
	for _, dir := range []string{"", "worker", "worker/src", "worker/test"} {
		tc.programs.visit(dir, nil)
	}
	tc.programs.record(programOf(t, "worker",
		listingOf("worker", "worker/src/index.ts")))
	tc.programs.record(programOf(t, "worker/test", "worker/src/index.ts\n"+
		viaLine("../src/index", "worker/test/a.test.ts", "")+
		"worker/test/a.test.ts\n"+includeLine("worker/test")))
	return c, tc
}

const poolRepoLock = `lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      '@vitest/coverage-istanbul':
        specifier: 4.1.11
        version: 4.1.11

  worker:
    devDependencies:
      '@cloudflare/vitest-pool-workers':
        specifier: 0.18.4
        version: 0.18.4
      vitest:
        specifier: 4.1.11
        version: 4.1.11

packages:

  '@cloudflare/vitest-pool-workers@0.18.4':
    resolution: {integrity: sha512-aaa}

  '@vitest/coverage-istanbul@4.1.11':
    resolution: {integrity: sha512-bbb}

  vitest@4.1.11:
    resolution: {integrity: sha512-ccc}

snapshots:

  '@cloudflare/vitest-pool-workers@0.18.4': {}

  '@vitest/coverage-istanbul@4.1.11': {}

  vitest@4.1.11: {}
`

const poolRepoLockNoIstanbul = `lockfileVersion: '9.0'

importers:

  .: {}

  worker:
    devDependencies:
      '@cloudflare/vitest-pool-workers':
        specifier: 0.18.4
        version: 0.18.4
      vitest:
        specifier: 4.1.11
        version: 4.1.11

packages:

  '@cloudflare/vitest-pool-workers@0.18.4':
    resolution: {integrity: sha512-aaa}

  vitest@4.1.11:
    resolution: {integrity: sha512-ccc}

snapshots:

  '@cloudflare/vitest-pool-workers@0.18.4': {}

  vitest@4.1.11: {}
`

const poolCfg = "worker/vitest.config.mts"

var poolEdge = importEdge(poolCfg, "@cloudflare/vitest-pool-workers",
	store+"@cloudflare/vitest-pool-workers/0.18.4/iii/node_modules/"+
		"@cloudflare/vitest-pool-workers/dist/index.d.ts")

var poolRules = []indexedRule{
	{kind: "ts_compile", name: "worker", pkg: "worker",
		srcs: []string{"src/index.ts"}},
	{kind: "ts_test", name: "test_test", pkg: "worker/test",
		srcs: []string{"a.test.ts"}},
}

// The pooled test's rule, resolved with the given config edges.
func resolvePooledTest(t *testing.T, c *config.Config, tc *tsConfig,
	edges []explainfiles.Edge) (*rule.Rule, string) {
	t.Helper()
	ix := buildIndex(t, c, poolRules...)
	s := tc.programs
	s.vitestEdges = map[string][]explainfiles.Edge{poolCfg: edges}
	imps := s.testImports(c.RepoRoot, tc.lock, "worker/test", "", poolCfg,
		s.srcs("worker/test", tc))
	return resolveEdgesOf(t, c, ix, "ts_test", "worker/test", "test_test", imps)
}

// The config's edge names the pool: the test names the filegroup over the
// wrangler config, runs istanbul coverage, and carries istanbul in D7 spelling.
func TestResolveEdges_WorkersPoolWritesTheTestsAttributes(t *testing.T) {
	c, tc := poolRepo(t, poolRepoLock)
	r, logged := resolvePooledTest(t, c, tc, []explainfiles.Edge{poolEdge})
	if got := r.AttrString("wrangler_config"); got != "//worker:wrangler_config" {
		t.Errorf("wrangler_config = %q, want //worker:wrangler_config", got)
	}
	if got := r.AttrString("coverage_provider"); got != "istanbul" {
		t.Errorf("coverage_provider = %q, want istanbul", got)
	}
	want := []string{"//worker", "@npm//:vitest_coverage-istanbul",
		"@npm//worker:cloudflare_vitest-pool-workers", "@npm//worker:vitest"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %q, want %q", got, want)
	}
	if logged != "" {
		t.Errorf("log, want nothing:\n%s", logged)
	}
}

// No pool edge: none of it, whatever the config names in a literal.
func TestResolveEdges_NoPoolEdgeWritesNoPoolAttributes(t *testing.T) {
	c, tc := poolRepo(t, poolRepoLock)
	r, _ := resolvePooledTest(t, c, tc, nil)
	for _, attr := range []string{"wrangler_config", "coverage_provider"} {
		if r.Attr(attr) != nil {
			t.Errorf("%s = %q, want unset: the config runs no pool", attr,
				r.AttrString(attr))
		}
	}
	want := []string{"//worker", "@npm//worker:cloudflare_vitest-pool-workers",
		"@npm//worker:vitest"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %q, want %q", got, want)
	}
}

// The pool with istanbul in no lockfile: wrangler_config comes, the provider
// and its dep stay off, and one line names the package to declare.
func TestResolveEdges_PoolWithoutIstanbulIsSaid(t *testing.T) {
	c, tc := poolRepo(t, poolRepoLockNoIstanbul)
	r, logged := resolvePooledTest(t, c, tc, []explainfiles.Edge{poolEdge})
	if got := r.AttrString("wrangler_config"); got != "//worker:wrangler_config" {
		t.Errorf("wrangler_config = %q, want //worker:wrangler_config", got)
	}
	if r.Attr("coverage_provider") != nil {
		t.Errorf("coverage_provider = %q, want unset: istanbul is in no lockfile",
			r.AttrString("coverage_provider"))
	}
	want := []string{"//worker", "@npm//worker:cloudflare_vitest-pool-workers",
		"@npm//worker:vitest"}
	if got := r.AttrStrings("deps"); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %q, want %q", got, want)
	}
	if !strings.Contains(logged, "@vitest/coverage-istanbul") ||
		strings.Count(logged, "\n") != 1 {
		t.Errorf("log, want one line naming @vitest/coverage-istanbul:\n%s", logged)
	}
}

// The config beside the tests: the wrangler config by its name in the test's
// own package, as `config` is.
func TestResolveEdges_SamePackagePoolNamesTheFile(t *testing.T) {
	c, tc := poolRepo(t, poolRepoLock)
	ix := buildIndex(t, c, poolRules...)
	s := tc.programs
	s.vitestEdges = map[string][]explainfiles.Edge{poolCfg: {poolEdge}}
	imps := s.testImports(c.RepoRoot, tc.lock, "worker", ":worker", poolCfg,
		s.srcs("worker", tc))
	r, _ := resolveEdgesOf(t, c, ix, "ts_test", "worker", "worker_test", imps)
	if got := r.AttrString("wrangler_config"); got != "wrangler.jsonc" {
		t.Errorf("wrangler_config = %q, want wrangler.jsonc", got)
	}
}
