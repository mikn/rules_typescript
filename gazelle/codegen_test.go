package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- helpers ---------------------------------------------------------------

// makeTcWithNpm builds a minimal tsConfig with the given npm packages loaded.
func makeTcWithNpm(pkgs ...string) *tsConfig {
	tc := &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		declarations:        "tsgo",
	}
	if len(pkgs) > 0 {
		tc.npmPackages = make(map[string]string, len(pkgs))
		for _, p := range pkgs {
			tc.npmPackages[p] = "@npm//:" + npmPackageToLabelName(p)
		}
	}
	return tc
}

// makeTcWithFramework builds a minimal tsConfig with a detected framework and
// no npmPackages map (simulates no lockfile loaded).
func makeTcWithFramework(f Framework) *tsConfig {
	return &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		declarations:        "tsgo",
		detectedFramework:   f,
	}
}

// ---- fileSet tests ---------------------------------------------------------

func TestFileSet_ContainsExpectedFiles(t *testing.T) {
	fs := fileSet([]string{"a.ts", "b.tsx", "schema.prisma"})
	if !fs["a.ts"] || !fs["b.tsx"] || !fs["schema.prisma"] {
		t.Error("fileSet missing expected keys")
	}
	if fs["c.ts"] {
		t.Error("fileSet should not contain c.ts")
	}
}

// ---- hasTsxFiles tests -----------------------------------------------------

func TestHasTsxFiles_WithNonGeneratedTsx(t *testing.T) {
	if !hasTsxFiles([]string{"index.tsx", "about.tsx"}) {
		t.Error("expected hasTsxFiles to return true for non-generated .tsx files")
	}
}

func TestHasTsxFiles_GeneratedTsxExcluded(t *testing.T) {
	// routeTree.gen.tsx is generated and should not count.
	if hasTsxFiles([]string{"routeTree.gen.tsx"}) {
		t.Error("expected hasTsxFiles to return false for .gen.tsx generated file")
	}
}

func TestHasTsxFiles_EmptyList(t *testing.T) {
	if hasTsxFiles(nil) {
		t.Error("expected hasTsxFiles to return false for empty list")
	}
}

func TestHasTsxFiles_OnlyTsNoTsx(t *testing.T) {
	if hasTsxFiles([]string{"index.ts", "utils.ts"}) {
		t.Error("expected hasTsxFiles to return false when only .ts files present")
	}
}

// ---- hasGraphQLFiles tests -------------------------------------------------

func TestHasGraphQLFiles_WithGraphQL(t *testing.T) {
	if !hasGraphQLFiles([]string{"schema.graphql", "query.gql"}) {
		t.Error("expected hasGraphQLFiles to return true")
	}
}

func TestHasGraphQLFiles_WithoutGraphQL(t *testing.T) {
	if hasGraphQLFiles([]string{"index.ts", "schema.json"}) {
		t.Error("expected hasGraphQLFiles to return false")
	}
}

// ---- hasCodegenConfig tests ------------------------------------------------

func TestHasCodegenConfig_YML(t *testing.T) {
	if !hasCodegenConfig(fileSet([]string{"codegen.yml"})) {
		t.Error("expected hasCodegenConfig to match codegen.yml")
	}
}

func TestHasCodegenConfig_TS(t *testing.T) {
	if !hasCodegenConfig(fileSet([]string{"codegen.ts"})) {
		t.Error("expected hasCodegenConfig to match codegen.ts")
	}
}

func TestHasCodegenConfig_Missing(t *testing.T) {
	if hasCodegenConfig(fileSet([]string{"schema.graphql"})) {
		t.Error("expected hasCodegenConfig to return false when no config file")
	}
}

// ---- openAPIFileName tests -------------------------------------------------

func TestOpenAPIFileName_YAML(t *testing.T) {
	got := openAPIFileName(fileSet([]string{"openapi.yaml"}))
	if got != "openapi.yaml" {
		t.Errorf("openAPIFileName: got %q, want %q", got, "openapi.yaml")
	}
}

func TestOpenAPIFileName_SwaggerJSON(t *testing.T) {
	got := openAPIFileName(fileSet([]string{"swagger.json"}))
	if got != "swagger.json" {
		t.Errorf("openAPIFileName: got %q, want %q", got, "swagger.json")
	}
}

func TestOpenAPIFileName_NotPresent(t *testing.T) {
	got := openAPIFileName(fileSet([]string{"schema.prisma"}))
	if got != "" {
		t.Errorf("openAPIFileName: expected empty string, got %q", got)
	}
}

// ---- npmBinLabel tests -----------------------------------------------------

func TestNpmBinLabel_Scoped(t *testing.T) {
	got := npmBinLabel("@graphql-codegen/cli")
	want := "@npm//:graphql-codegen_cli_bin"
	if got != want {
		t.Errorf("npmBinLabel(@graphql-codegen/cli): got %q, want %q", got, want)
	}
}

func TestNpmBinLabel_Unscoped(t *testing.T) {
	got := npmBinLabel("prisma")
	want := "@npm//:prisma_bin"
	if got != want {
		t.Errorf("npmBinLabel(prisma): got %q, want %q", got, want)
	}
}

func TestNpmBinLabel_OpenAPITypescript(t *testing.T) {
	got := npmBinLabel("openapi-typescript")
	want := "@npm//:openapi-typescript_bin"
	if got != want {
		t.Errorf("npmBinLabel(openapi-typescript): got %q, want %q", got, want)
	}
}

// ---- TanStack routes ------------------------------------------------------

// A TanStack routes directory gets NO ts_codegen. The Start Vite plugin writes
// the route tree into the staging directory during the bundle, and the rule
// this used to emit named a generator label that has never existed in the
// ruleset, so it could not build.
func TestDetectCodegen_NoTanStackRouteTree(t *testing.T) {
	for _, pkg := range []string{"@tanstack/react-router", "@tanstack/react-start", "@tanstack/start"} {
		tc := makeTcWithNpm(pkg)
		got := detectCodegen("src/routes", []string{"index.tsx", "about.tsx"}, tc)
		if len(got) != 0 {
			t.Errorf("detectCodegen with %s: got %d patterns, want none", pkg, len(got))
		}
	}
}

// ---- detectPrisma tests ----------------------------------------------------

func TestDetectPrisma_Detected(t *testing.T) {
	tc := makeTcWithNpm("prisma")
	p := detectPrisma(fileSet([]string{"schema.prisma"}), tc)
	if p == nil {
		t.Fatal("expected detectPrisma to return a pattern, got nil")
	}
	if p.Name != "prisma_client" {
		t.Errorf("Name: got %q, want %q", p.Name, "prisma_client")
	}
	if p.OutDir != "generated/client" {
		t.Errorf("OutDir: got %q, want %q", p.OutDir, "generated/client")
	}
	if p.Generator != "@npm//:prisma_bin" {
		t.Errorf("Generator: got %q", p.Generator)
	}
}

func TestDetectPrisma_PrismaClientPackage(t *testing.T) {
	// @prisma/client also triggers detection.
	tc := makeTcWithNpm("@prisma/client")
	p := detectPrisma(fileSet([]string{"schema.prisma"}), tc)
	if p == nil {
		t.Fatal("expected detection with @prisma/client package")
	}
}

func TestDetectPrisma_NoSchemaPrisma(t *testing.T) {
	tc := makeTcWithNpm("prisma")
	p := detectPrisma(fileSet([]string{"index.ts"}), tc)
	if p != nil {
		t.Error("expected nil when schema.prisma is absent")
	}
}

func TestDetectPrisma_MissingNpmPackage(t *testing.T) {
	tc := makeTcWithNpm("react") // prisma not in deps
	p := detectPrisma(fileSet([]string{"schema.prisma"}), tc)
	if p != nil {
		t.Error("expected nil when prisma not in npm deps")
	}
}

func TestDetectPrisma_NoLockfileSchemaPresent(t *testing.T) {
	// No npmPackages map — schema.prisma alone is enough.
	tc := &tsConfig{declarations: "tsgo"}
	p := detectPrisma(fileSet([]string{"schema.prisma"}), tc)
	if p == nil {
		t.Fatal("expected detection when schema.prisma present and npmPackages is nil")
	}
}

// ---- detectGraphQLCodegen tests --------------------------------------------

func TestDetectGraphQLCodegen_Detected(t *testing.T) {
	tc := makeTcWithNpm("@graphql-codegen/cli")
	files := []string{"schema.graphql", "queries.gql", "codegen.yml"}
	p := detectGraphQLCodegen(files, fileSet(files), tc)
	if p == nil {
		t.Fatal("expected detectGraphQLCodegen to return a pattern, got nil")
	}
	if p.Name != "graphql_types" {
		t.Errorf("Name: got %q, want %q", p.Name, "graphql_types")
	}
	if len(p.Outs) == 0 || p.Outs[0] != "generated/graphql.ts" {
		t.Errorf("Outs: got %v, want [generated/graphql.ts]", p.Outs)
	}
}

func TestDetectGraphQLCodegen_NoGraphQLFiles(t *testing.T) {
	tc := makeTcWithNpm("@graphql-codegen/cli")
	files := []string{"codegen.yml"}
	p := detectGraphQLCodegen(files, fileSet(files), tc)
	if p != nil {
		t.Error("expected nil when no .graphql files present")
	}
}

func TestDetectGraphQLCodegen_NoConfig(t *testing.T) {
	tc := makeTcWithNpm("@graphql-codegen/cli")
	files := []string{"schema.graphql"}
	p := detectGraphQLCodegen(files, fileSet(files), tc)
	if p != nil {
		t.Error("expected nil when no codegen config file present")
	}
}

func TestDetectGraphQLCodegen_MissingNpmPackage(t *testing.T) {
	tc := makeTcWithNpm("react")
	files := []string{"schema.graphql", "codegen.yml"}
	p := detectGraphQLCodegen(files, fileSet(files), tc)
	if p != nil {
		t.Error("expected nil when @graphql-codegen/cli not in npm deps")
	}
}

func TestDetectGraphQLCodegen_SrcsContainConfigFile(t *testing.T) {
	tc := makeTcWithNpm("@graphql-codegen/cli")
	files := []string{"schema.graphql", "codegen.ts"}
	p := detectGraphQLCodegen(files, fileSet(files), tc)
	if p == nil {
		t.Fatal("expected detection with codegen.ts config")
	}
	// The config file should appear in srcs.
	foundConfig := false
	for _, s := range p.Srcs {
		if s == "codegen.ts" {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Errorf("expected codegen.ts in srcs, got %v", p.Srcs)
	}
}

// ---- detectOpenAPI tests ---------------------------------------------------

func TestDetectOpenAPI_YAML(t *testing.T) {
	tc := makeTcWithNpm("openapi-typescript")
	p := detectOpenAPI(fileSet([]string{"openapi.yaml"}), tc)
	if p == nil {
		t.Fatal("expected detectOpenAPI to return a pattern for openapi.yaml")
	}
	if p.Name != "api_types" {
		t.Errorf("Name: got %q, want %q", p.Name, "api_types")
	}
	if len(p.Srcs) == 0 || p.Srcs[0] != "openapi.yaml" {
		t.Errorf("Srcs: got %v, want [openapi.yaml]", p.Srcs)
	}
	if len(p.Outs) == 0 || p.Outs[0] != "api-types.ts" {
		t.Errorf("Outs: got %v, want [api-types.ts]", p.Outs)
	}
	if p.Generator != "@npm//:openapi-typescript_bin" {
		t.Errorf("Generator: got %q", p.Generator)
	}
}

func TestDetectOpenAPI_SwaggerJSON(t *testing.T) {
	tc := makeTcWithNpm("openapi-typescript")
	p := detectOpenAPI(fileSet([]string{"swagger.json"}), tc)
	if p == nil {
		t.Fatal("expected detection for swagger.json")
	}
	if p.Srcs[0] != "swagger.json" {
		t.Errorf("Srcs[0]: got %q, want %q", p.Srcs[0], "swagger.json")
	}
}

func TestDetectOpenAPI_NoSpecFile(t *testing.T) {
	tc := makeTcWithNpm("openapi-typescript")
	p := detectOpenAPI(fileSet([]string{"index.ts"}), tc)
	if p != nil {
		t.Error("expected nil when no OpenAPI spec file present")
	}
}

func TestDetectOpenAPI_MissingNpmPackage(t *testing.T) {
	tc := makeTcWithNpm("react")
	p := detectOpenAPI(fileSet([]string{"openapi.yaml"}), tc)
	if p != nil {
		t.Error("expected nil when openapi-typescript not in npm deps")
	}
}

func TestDetectOpenAPI_NoNpmMap(t *testing.T) {
	// No npm package map — spec file alone triggers detection.
	tc := &tsConfig{declarations: "tsgo"}
	p := detectOpenAPI(fileSet([]string{"openapi.json"}), tc)
	if p == nil {
		t.Fatal("expected detection when npmPackages is nil and openapi.json present")
	}
}

// ---- detectCodegen (master) tests ------------------------------------------

func TestDetectCodegen_PrismaAndOpenAPIInSameDir(t *testing.T) {
	// Unlikely in practice but verify both detectors fire independently.
	tc := makeTcWithNpm("prisma", "openapi-typescript")
	files := []string{"schema.prisma", "openapi.yaml"}
	patterns := detectCodegen("mypackage", files, tc)
	if len(patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d: %v", len(patterns), patterns)
	}
}

func TestDetectCodegen_Empty(t *testing.T) {
	tc := makeTcWithNpm("react")
	files := []string{"index.tsx", "utils.ts"}
	patterns := detectCodegen("src", files, tc)
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns, got %d: %v", len(patterns), patterns)
	}
}

func TestDetectCodegen_CustomDirectivesIncluded(t *testing.T) {
	tc := makeTcWithNpm("react")
	tc.customCodegens = []CodegenPattern{
		{
			Name:      "my_gen",
			Srcs:      []string{"input.ts"},
			Outs:      []string{"output.ts"},
			Generator: "@npm//:my-tool_bin",
			Dir:       "src",
		},
	}
	patterns := detectCodegen("src", []string{"input.ts"}, tc)
	if len(patterns) != 1 || patterns[0].Name != "my_gen" {
		t.Errorf("expected custom codegen pattern, got %v", patterns)
	}
}

// ---- parseCodegenDirective tests -------------------------------------------

func TestParseCodegenDirective_BasicSingleOut(t *testing.T) {
	cp := parseCodegenDirective("", "api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}")
	if cp == nil {
		t.Fatal("expected non-nil result")
	}
	if cp.Name != "api_types" {
		t.Errorf("Name: got %q, want api_types", cp.Name)
	}
	if cp.Generator != "@npm//:openapi-typescript_bin" {
		t.Errorf("Generator: got %q", cp.Generator)
	}
	if len(cp.Outs) != 1 || cp.Outs[0] != "api-types.ts" {
		t.Errorf("Outs: got %v, want [api-types.ts]", cp.Outs)
	}
	if len(cp.Args) != 3 || cp.Args[0] != "{srcs}" || cp.Args[1] != "-o" || cp.Args[2] != "{out}" {
		t.Errorf("Args: got %v, want [{srcs} -o {out}]", cp.Args)
	}
}

func TestParseCodegenDirective_MultipleOuts(t *testing.T) {
	cp := parseCodegenDirective("", "my_gen @npm//:tool_bin types.ts,client.ts generate")
	if cp == nil {
		t.Fatal("expected non-nil result")
	}
	if len(cp.Outs) != 2 {
		t.Fatalf("Outs: got %v, want [types.ts, client.ts]", cp.Outs)
	}
	if cp.Outs[0] != "types.ts" || cp.Outs[1] != "client.ts" {
		t.Errorf("Outs: got %v", cp.Outs)
	}
}

func TestParseCodegenDirective_DirOutput(t *testing.T) {
	cp := parseCodegenDirective("", "prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}")
	if cp == nil {
		t.Fatal("expected non-nil result")
	}
	if cp.OutDir != "generated/client" {
		t.Errorf("OutDir: got %q, want generated/client", cp.OutDir)
	}
	if len(cp.Outs) != 0 {
		t.Errorf("Outs should be empty when out_dir is set, got %v", cp.Outs)
	}
	if len(cp.Args) != 3 {
		t.Errorf("Args: got %v, want [generate, --schema, {srcs}]", cp.Args)
	}
}

func TestParseCodegenDirective_TooFewFields(t *testing.T) {
	// Only 2 fields — needs at least 3.
	cp := parseCodegenDirective("", "my_gen @npm//:tool_bin")
	if cp != nil {
		t.Errorf("expected nil for directive with too few fields, got %+v", cp)
	}
}

func TestParseCodegenDirective_EmptyValue(t *testing.T) {
	cp := parseCodegenDirective("", "")
	if cp != nil {
		t.Errorf("expected nil for empty directive value")
	}
}

func TestParseCodegenDirective_EmptyDirAfterPrefix(t *testing.T) {
	// "dir:" with nothing after it should fail.
	cp := parseCodegenDirective("", "my_gen @npm//:tool_bin dir: generate")
	if cp != nil {
		t.Errorf("expected nil for empty dir: value")
	}
}

func TestParseCodegenDirective_ArgsOptional(t *testing.T) {
	// No args — only name + generator + outs.
	cp := parseCodegenDirective("", "gen @npm//:tool_bin output.ts")
	if cp == nil {
		t.Fatal("expected non-nil result when args omitted")
	}
	if len(cp.Args) != 0 {
		t.Errorf("Args: expected empty, got %v", cp.Args)
	}
}

// ---- ts_codegen directive integration tests --------------------------------

func TestDirective_Codegen_SingleTarget(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveCodegen, "api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}"),
	})
	if len(tc.customCodegens) != 1 {
		t.Fatalf("expected 1 custom codegen, got %d: %v", len(tc.customCodegens), tc.customCodegens)
	}
	cp := tc.customCodegens[0]
	if cp.Name != "api_types" {
		t.Errorf("Name: got %q, want api_types", cp.Name)
	}
	if cp.Generator != "@npm//:openapi-typescript_bin" {
		t.Errorf("Generator: got %q", cp.Generator)
	}
}

func TestDirective_Codegen_MultipleDirectives(t *testing.T) {
	tc := makeConfig("", []rule.Directive{
		directive(directiveCodegen, "gen1 @npm//:tool1_bin out1.ts"),
		directive(directiveCodegen, "gen2 @npm//:tool2_bin out2.ts"),
	})
	if len(tc.customCodegens) != 2 {
		t.Fatalf("expected 2 custom codegens, got %d", len(tc.customCodegens))
	}
}

func TestDirective_Codegen_InvalidDirective_IsIgnored(t *testing.T) {
	// Malformed directive (too few fields) should not panic and produce no entry.
	tc := makeConfig("", []rule.Directive{
		directive(directiveCodegen, "only_two_fields @npm//:tool_bin"),
	})
	if len(tc.customCodegens) != 0 {
		t.Errorf("expected 0 custom codegens for malformed directive, got %d", len(tc.customCodegens))
	}
}

func TestDirective_Codegen_InheritedByChild(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directiveCodegen, "gen1 @npm//:tool_bin out.ts")},
		"src",
		nil,
	)
	if len(tc.customCodegens) != 1 {
		t.Errorf("child should inherit parent's custom codegen, got %d", len(tc.customCodegens))
	}
}

func TestDirective_Codegen_ChildCanAddToParent(t *testing.T) {
	tc := makeChildConfig(
		[]rule.Directive{directive(directiveCodegen, "gen1 @npm//:tool1_bin out1.ts")},
		"src",
		[]rule.Directive{directive(directiveCodegen, "gen2 @npm//:tool2_bin out2.ts")},
	)
	if len(tc.customCodegens) != 2 {
		t.Fatalf("expected 2 custom codegens (parent + child), got %d: %v", len(tc.customCodegens), tc.customCodegens)
	}
}

// ---- the generated-code workflow -------------------------------------------

// indexGenerated puts a generate result's rules in one BUILD file and indexes
// them, which is what lets a src label reach the ts_codegen it names.
func indexGenerated(t *testing.T, c *config.Config, pkg string, res language.GenerateResult) *resolve.RuleIndex {
	t.Helper()
	f := rule.EmptyFile("BUILD.bazel", pkg)
	for _, r := range res.Gen {
		r.Insert(f)
	}
	lang := &tsLang{}
	ix := resolve.NewRuleIndex(func(*rule.Rule, string) resolve.Resolver { return lang })
	for _, r := range f.Rules {
		ix.AddRule(c, r, f)
	}
	ix.Finish()
	return ix
}

// depsFor resolves one import from the repository root package against ix.
func depsFor(t *testing.T, c *config.Config, ix *resolve.RuleIndex, imp string) []string {
	t.Helper()
	r := rule.NewRule("ts_compile", "app")
	resolveImports(c, ix, r, []string{imp}, label.New("", "", "app"))
	return r.AttrStrings("deps")
}

// The directive names a target, so a run has to write one. Parsing it into a
// CodegenPattern that never reaches a BUILD file is the whole bug.
func TestGenerate_CodegenDirectiveEmitsTheRule(t *testing.T) {
	res := runGenerateWithBuild(t, "api", `
# gazelle:ts_codegen schema_gen //tools:schemagen schema.gen.ts --out {out} --srcs {srcs}
`, map[string]string{
		"client.ts": "import type { S } from './schema.gen';\nexport type C = S;\n",
	})

	r := generatedRule(res, "schema_gen")
	if r == nil {
		t.Fatalf("no ts_codegen for the directive; generated %v", generatedNames(t, res))
	}
	if r.Kind() != "ts_codegen" {
		t.Errorf("schema_gen kind = %s, want ts_codegen", r.Kind())
	}
	if got, want := r.AttrStrings("outs"), []string{"schema.gen.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("outs = %v, want %v", got, want)
	}
	if got := r.AttrString("generator"); got != "//tools:schemagen" {
		t.Errorf("generator = %q, want //tools:schemagen", got)
	}
	if got, want := r.AttrStrings("srcs"), []string{"client.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("srcs = %v, want %v (the directory's own sources)", got, want)
	}
}

// A directive reaches every child directory, but the target it names belongs in
// the one package it was written in.
func TestDetectCodegen_DirectiveOnlyFiresInItsOwnDirectory(t *testing.T) {
	tc := makeTcWithNpm("react")
	tc.customCodegens = []CodegenPattern{{
		Name:      "my_gen",
		Srcs:      []string{"input.ts"},
		Outs:      []string{"output.ts"},
		Generator: "@npm//:my-tool_bin",
		Dir:       "src",
	}}
	if got := detectCodegen("src/nested", []string{"input.ts"}, tc); len(got) != 0 {
		t.Errorf("child directory got %v, want no patterns", got)
	}
}

// The generated module has to reach a compile through srcs -- ts_compile deps
// take JsInfo, which ts_codegen does not return -- so the import resolves to
// the compile that carries the codegen label.
func TestGenerate_ImportOfADeclaredButAbsentModuleResolves(t *testing.T) {
	res := runGenerateWithBuild(t, "api", `
# gazelle:ts_codegen schema_gen //tools:schemagen schema.gen.ts --out {out} --srcs {srcs}
`, map[string]string{
		"client.ts": "import type { S } from './schema.gen';\nexport type C = S;\n",
	})

	compile := generatedRule(res, "schema_gen_compile")
	if compile == nil {
		t.Fatalf("no companion ts_compile generated; got %v", generatedNames(t, res))
	}
	assertRule(t, generatedNames(t, res), "schema_gen_compile", "ts_compile")
	if got, want := compile.AttrStrings("srcs"), []string{":schema_gen"}; !reflect.DeepEqual(got, want) {
		t.Errorf("companion srcs = %v, want %v", got, want)
	}
	if got := compile.AttrString("declarations"); got != "oxc" {
		t.Errorf("companion declarations = %q, want oxc", got)
	}

	c := emptyConfig()
	got := depsFor(t, c, indexGenerated(t, c, "api", res), "./api/schema.gen")
	if want := []string{"//api:schema_gen_compile"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deps for ./api/schema.gen = %v, want %v", got, want)
	}
}

// The out is checked in until the day it is not, and a file that is both a
// source and an output is a declaration Bazel rejects outright.
func TestGenerate_CodegenOutOnDiskIsNotAlsoASrc(t *testing.T) {
	res := runGenerateWithBuild(t, "api", `
# gazelle:ts_codegen schema_gen //tools:schemagen schema.ts srcs:schema.graphql --out {out}
`, map[string]string{
		"schema.graphql": "type Query { a: Int }\n",
		"schema.ts":      "export type S = number;\n",
		"client.ts":      "import type { S } from './schema';\nexport type C = S;\n",
	})

	compile := generatedRule(res, "api")
	if compile == nil {
		t.Fatalf("no ts_compile generated; got %v", generatedNames(t, res))
	}
	if got, want := compile.AttrStrings("srcs"), []string{"client.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_compile srcs = %v, want %v", got, want)
	}

	gen := generatedRule(res, "schema_gen")
	if gen == nil {
		t.Fatalf("no ts_codegen for the directive; generated %v", generatedNames(t, res))
	}
	if got, want := gen.AttrStrings("srcs"), []string{"schema.graphql"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_codegen srcs = %v, want %v", got, want)
	}

	c := emptyConfig()
	got := depsFor(t, c, indexGenerated(t, c, "api", res), "./api/schema")
	if want := []string{"//api:schema_gen_compile"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deps for ./api/schema = %v, want %v", got, want)
	}
}

// A *.gen.ts no rule declares is a checked-in file like any other: nothing in
// the build writes it, so leaving it out is a module its importers cannot find.
func TestGenerate_UndeclaredGeneratedFileIsASrc(t *testing.T) {
	res := runGenerate(t, "api", map[string]string{
		"client.ts":     "export const a = 1;\n",
		"schema.gen.ts": "export type S = number;\n",
	})

	compile := generatedRule(res, "api")
	if compile == nil {
		t.Fatalf("no ts_compile generated; got %v", generatedNames(t, res))
	}
	if got, want := compile.AttrStrings("srcs"), []string{"client.ts", "schema.gen.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_compile srcs = %v, want %v", got, want)
	}
	for _, r := range res.Gen {
		if r.Kind() == "ts_codegen" {
			t.Errorf("ts_codegen %q generated for an undeclared *.gen.ts", r.Name())
		}
	}
}

// The rule a previous run wrote is the same claim as the pattern behind it, and
// the second run has only the BUILD file to read it from.
func TestGenerate_OutOfAnExistingCodegenRuleIsNotASrc(t *testing.T) {
	res := runGenerateWithBuild(t, "api", `
ts_codegen(
    name = "schema_gen",
    srcs = ["schema.graphql"],
    outs = ["schema.ts"],
    generator = "//tools:schemagen",
)
`, map[string]string{
		"schema.graphql": "type Query { a: Int }\n",
		"schema.ts":      "export type S = number;\n",
		"client.ts":      "import type { S } from './schema';\nexport type C = S;\n",
	})

	compile := generatedRule(res, "api")
	if compile == nil {
		t.Fatalf("no ts_compile generated; got %v", generatedNames(t, res))
	}
	if got, want := compile.AttrStrings("srcs"), []string{"client.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ts_compile srcs = %v, want %v", got, want)
	}
}

// The bug this whole change is about is a directive that does nothing without
// saying so, and a directive whose srcs default finds nothing is that again.
func TestGenerate_CodegenWithNothingToReadIsReported(t *testing.T) {
	logged := captureLog(t, func() {
		runGenerateWithBuild(t, "api", `
# gazelle:ts_codegen schema_gen //tools:schemagen schema.gen.ts --out {out}
`, map[string]string{"schema.graphql": "type Query { a: Int }\n"})
	})
	if !strings.Contains(logged, "schema_gen") {
		t.Errorf("no warning for a codegen with no srcs to read; logged %q", logged)
	}
}

// A package whose only TypeScript is generated: nothing on disk to make it a
// boundary, and the targets still have to appear.
func TestGenerate_PackageWithOnlyGeneratedSources(t *testing.T) {
	res := runGenerateWithBuild(t, "api", `
# gazelle:ts_codegen schema_gen //tools:schemagen schema.gen.ts srcs:schema.graphql --out {out}
`, map[string]string{"schema.graphql": "type Query { a: Int }\n"})

	byName := generatedNames(t, res)
	assertRule(t, byName, "schema_gen", "ts_codegen")
	assertRule(t, byName, "schema_gen_compile", "ts_compile")

	c := emptyConfig()
	got := depsFor(t, c, indexGenerated(t, c, "api", res), "./api/schema.gen")
	if want := []string{"//api:schema_gen_compile"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deps for ./api/schema.gen = %v, want %v", got, want)
	}
}

// The claim has to hold wherever the file turns up. In tsconfig mode a
// subdirectory is not a package, so its files are rolled into this one -- and a
// rolled-up file a ts_codegen declares would otherwise reach a css_library and
// be a source and an output of the same package.
func TestGenerate_RolledUpCodegenOutIsNotAlsoASrc(t *testing.T) {
	res := runGenerateWithBuild(t, "api", `
# gazelle:ts_package_boundary tsconfig
# gazelle:ts_codegen theme_gen //tools:themegen sub/theme.css srcs:tokens.json --out {out}
`, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"index.ts":      "export const a = 1;\n",
		"tokens.json":   "{}\n",
		"sub/theme.css": ".a {}\n",
	})

	for _, r := range res.Gen {
		if r.Kind() != "css_library" {
			continue
		}
		for _, src := range r.AttrStrings("srcs") {
			if src == "sub/theme.css" {
				t.Errorf("css_library %q compiles sub/theme.css, which ts_codegen theme_gen declares as an out", r.Name())
			}
		}
	}
	if gen := generatedRule(res, "theme_gen"); gen == nil {
		t.Fatalf("no ts_codegen for the directive; generated %v", generatedNames(t, res))
	}
}

// The whole cycle, not the pieces: generate, merge, index, resolve, write. An
// import of a module only the generator will ever produce has to come out of it
// as a dep, and since an unclassified extension now resolves to nothing rather
// than to a fabricated package, a miss here is silence instead of a build
// failure that names the file.
func TestConverge_GeneratedModuleResolvesThroughTheWholeCycle(t *testing.T) {
	repoRoot := t.TempDir()
	for name, content := range map[string]string{
		"BUILD.bazel":        "",
		"api/BUILD.bazel":    "# gazelle:ts_codegen schema_gen //tools:schemagen schema.gen.ts srcs:schema.graphql --out {out}\n",
		"api/schema.graphql": "type Query { a: Int }\n",
		"api/client.ts":      "import type { S } from \"./schema.gen\";\nexport type C = S;\n",
	} {
		full := filepath.Join(repoRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	convergeGazelle(t, repoRoot)

	data, err := os.ReadFile(filepath.Join(repoRoot, "api", "BUILD.bazel"))
	if err != nil {
		t.Fatal(err)
	}
	build := string(data)
	for _, want := range []string{
		`ts_codegen(`,
		`name = "schema_gen"`,
		`name = "schema_gen_compile"`,
		`":schema_gen_compile"`,
	} {
		if !strings.Contains(build, want) {
			t.Errorf("api/BUILD.bazel is missing %s:\n%s", want, build)
		}
	}
}

const catalogueDirective = "# gazelle:ts_codegen messages //tools:paraglide dir:compiled " +
	"srcs:settings.json,glob([\"messages/*.json\"]) --outdir {out}\n"

func convergeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	repoRoot := t.TempDir()
	for name, content := range files {
		full := filepath.Join(repoRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	convergeGazelle(t, repoRoot)
	return repoRoot
}

// A directive naming a glob has to reach the BUILD file as Starlark. Quoted,
// it is a string on a label_list attr, which Bazel refuses to load at all --
// so the directive's own inputs decide whether the package parses.
func TestConverge_CodegenDirectiveWritesAGlobAsStarlark(t *testing.T) {
	repoRoot := convergeTree(t, map[string]string{
		"BUILD.bazel":          "",
		"web/BUILD.bazel":      catalogueDirective,
		"web/settings.json":    "{}\n",
		"web/messages/en.json": "{}\n",
		"web/app.ts":           "export const x = 1;\n",
	})

	data, err := os.ReadFile(filepath.Join(repoRoot, "web", "BUILD.bazel"))
	if err != nil {
		t.Fatal(err)
	}
	build := string(data)
	if !strings.Contains(build, `srcs = ["settings.json"] + glob(["messages/*.json"])`) {
		t.Errorf("web/BUILD.bazel does not carry the directive's srcs as Starlark:\n%s", build)
	}
	if strings.Contains(build, `"glob(`) {
		t.Errorf("the glob reached the BUILD file quoted, which Bazel rejects:\n%s", build)
	}
}

// Writing the glob correctly is not enough for //web to load: a json_library
// per catalogue makes messages/ a package, glob() does not descend into one,
// and Bazel rejects a package whose glob matched nothing. The catalogues are
// the ancestor rule's inputs, so they get no targets of their own.
func TestConverge_ACodegenGlobLeavesItsSubdirectoryUnpackaged(t *testing.T) {
	repoRoot := convergeTree(t, map[string]string{
		"BUILD.bazel":          "",
		"web/BUILD.bazel":      catalogueDirective,
		"web/settings.json":    "{}\n",
		"web/messages/en.json": "{}\n",
		"web/messages/sv.json": "{}\n",
		"web/app.ts":           "export const x = 1;\n",
	})

	sub := filepath.Join(repoRoot, "web", "messages", "BUILD.bazel")
	if data, err := os.ReadFile(sub); err == nil {
		t.Errorf("web/messages is its own package, so //web's glob matches nothing:\n%s", data)
	}
}

// Only when nothing is left over: a file the glob does not collect still needs
// a target, that target still makes a package, and deleting it would cost the
// build something without buying the ancestor its glob back.
func TestConverge_AnUnglobbedFileKeepsTheSubdirectoryAPackage(t *testing.T) {
	repoRoot := convergeTree(t, map[string]string{
		"BUILD.bazel":            "",
		"web/BUILD.bazel":        catalogueDirective,
		"web/settings.json":      "{}\n",
		"web/messages/en.json":   "{}\n",
		"web/messages/helper.ts": "export const y = 2;\n",
		"web/app.ts":             "export const x = 1;\n",
	})

	data, err := os.ReadFile(filepath.Join(repoRoot, "web", "messages", "BUILD.bazel"))
	if err != nil {
		t.Fatalf("web/messages/helper.ts lost its target: %v", err)
	}
	if !strings.Contains(string(data), "helper.ts") {
		t.Errorf("web/messages/BUILD.bazel does not compile helper.ts:\n%s", data)
	}
}

func TestCodegenGlobClaims(t *testing.T) {
	tc := &tsConfig{customCodegens: []CodegenPattern{{
		Name: "messages", Dir: "web",
		Srcs: []string{"settings.json", `glob(["messages/*.json"],exclude=["messages/_*.json"])`},
	}}}
	files := []string{"en.json", "_draft.json", "notes.md"}

	claimed := codegenGlobClaims("web/messages", files, tc)
	want := map[string]struct{}{"en.json": {}, "_draft.json": {}}
	if !reflect.DeepEqual(claimed, want) {
		t.Errorf("claims in web/messages = %v, want %v", claimed, want)
	}
	if claimed := codegenGlobClaims("web/other", files, tc); len(claimed) > 0 {
		t.Errorf("claims in web/other = %v, want none", claimed)
	}
	if claimed := codegenGlobClaims("web", []string{"settings.json"}, tc); len(claimed) > 0 {
		t.Errorf("claims in the declaring directory = %v, want none", claimed)
	}
}

// The srcs field is comma-separated, and a glob's own patterns are too.
func TestParseCodegenDirective_GlobKeepsItsOwnCommas(t *testing.T) {
	cp := parseCodegenDirective("web", `messages //tools:gen dir:compiled srcs:glob(["a/*.json","b/*.json"]) --outdir {out}`)
	if cp == nil {
		t.Fatal("directive rejected")
	}
	want := []string{`glob(["a/*.json","b/*.json"])`}
	if !reflect.DeepEqual(cp.Srcs, want) {
		t.Errorf("Srcs = %v, want %v", cp.Srcs, want)
	}
	if got, want := cp.Args, []string{"--outdir", "{out}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Args = %v, want %v", got, want)
	}
}

// A srcs entry that opens with "glob(" and is not one is refused outright: the
// alternative is a rule whose generator reads fewer inputs than were named.
func TestBuildCodegenRule_RefusesAnUnparseableGlob(t *testing.T) {
	if r := buildCodegenRule(CodegenPattern{
		Name:      "messages",
		Generator: "//tools:gen",
		OutDir:    "compiled",
		Srcs:      []string{"settings.json", `glob(["a/*.json"`},
	}); r != nil {
		t.Errorf("rule emitted with srcs %v; want none", r.Attr("srcs"))
	}
}

// out_dir declares one directory artifact, so no file in it has a label and no
// import can name one. Writing a compile over it would only fail at analysis.
func TestGenerate_OutDirCodegenGetsNoCompanionCompile(t *testing.T) {
	res := runGenerateWithBuild(t, "web", `
# gazelle:ts_codegen messages //tools:paraglide dir:compiled srcs:settings.json --outdir {out}
`, map[string]string{
		"app.ts":        "export const x = 1;\n",
		"settings.json": "{}\n",
	})
	if generatedRule(res, "messages") == nil {
		t.Fatalf("no ts_codegen for the directive; generated %v", generatedNames(t, res))
	}
	if r := generatedRule(res, "messages_compile"); r != nil {
		t.Errorf("companion %s generated over an out_dir codegen", r.Kind())
	}
}

// The directive is split on whitespace before anything else, so a glob's
// patterns have to be written without a space after the comma. Written with
// one, the entry is truncated -- and a truncated glob emits nothing rather than
// a target Bazel cannot load.
func TestBuildCodegenRule_RefusesAGlobBrokenByWhitespace(t *testing.T) {
	cp := parseCodegenDirective("web", `messages //tools:gen dir:compiled srcs:glob(["a/*.json", "b/*.json"]) --outdir {out}`)
	if cp == nil {
		t.Fatal("directive rejected outright; want it refused when the rule is built")
	}
	if r := buildCodegenRule(*cp); r != nil {
		t.Errorf("rule emitted from a truncated glob, srcs %v", r.Attr("srcs"))
	}
}
