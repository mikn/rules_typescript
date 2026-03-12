package typescript

import (
	"testing"

	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- helpers ---------------------------------------------------------------

// makeTcWithNpm builds a minimal tsConfig with the given npm packages loaded.
func makeTcWithNpm(pkgs ...string) *tsConfig {
	tc := &tsConfig{
		packageBoundaryMode:  boundaryEveryDir,
		isolatedDeclarations: true,
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
		packageBoundaryMode:  boundaryEveryDir,
		isolatedDeclarations: true,
		detectedFramework:    f,
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

// ---- isInsideRoutesSegment tests -------------------------------------------

func TestIsInsideRoutesSegment_True(t *testing.T) {
	cases := []string{
		"src/routes",
		"src/routes/users",
		"app/routes/posts/$id",
	}
	for _, rel := range cases {
		if !isInsideRoutesSegment(rel) {
			t.Errorf("isInsideRoutesSegment(%q): expected true", rel)
		}
	}
}

func TestIsInsideRoutesSegment_False(t *testing.T) {
	cases := []string{
		"src/components",
		"src/router", // "router" != "routes"
		"routes-config",
	}
	for _, rel := range cases {
		if isInsideRoutesSegment(rel) {
			t.Errorf("isInsideRoutesSegment(%q): expected false", rel)
		}
	}
}

// ---- isRoutesRoot tests ----------------------------------------------------

func TestIsRoutesRoot_True(t *testing.T) {
	cases := []string{
		"routes",
		"src/routes",
		"app/routes",
	}
	for _, rel := range cases {
		if !isRoutesRoot(rel) {
			t.Errorf("isRoutesRoot(%q): expected true", rel)
		}
	}
}

func TestIsRoutesRoot_False(t *testing.T) {
	cases := []string{
		"src/routes/users",
		"src/routes/posts/$id",
	}
	for _, rel := range cases {
		if isRoutesRoot(rel) {
			t.Errorf("isRoutesRoot(%q): expected false", rel)
		}
	}
}

// ---- detectTanStackRoutes tests --------------------------------------------

func TestDetectTanStackRoutes_Detected(t *testing.T) {
	tc := makeTcWithNpm("@tanstack/react-router")
	files := []string{"index.tsx", "about.tsx", "routeTree.gen.ts"}
	p := detectTanStackRoutes("src/routes", files, tc)
	if p == nil {
		t.Fatal("expected detectTanStackRoutes to return a pattern, got nil")
	}
	if p.Name != "route_tree" {
		t.Errorf("Name: got %q, want %q", p.Name, "route_tree")
	}
	if len(p.Outs) != 1 || p.Outs[0] != "routeTree.gen.ts" {
		t.Errorf("Outs: got %v, want [routeTree.gen.ts]", p.Outs)
	}
	if p.Generator != "@rules_typescript//tools/codegen:tanstack_routes" {
		t.Errorf("Generator: got %q", p.Generator)
	}
}

func TestDetectTanStackRoutes_TanStackStart(t *testing.T) {
	tc := makeTcWithNpm("@tanstack/start")
	p := detectTanStackRoutes("src/routes", []string{"index.tsx"}, tc)
	if p == nil {
		t.Fatal("expected detection with @tanstack/start npm package")
	}
}

func TestDetectTanStackRoutes_NotInsideRoutesDir(t *testing.T) {
	tc := makeTcWithNpm("@tanstack/react-router")
	p := detectTanStackRoutes("src/components", []string{"Button.tsx"}, tc)
	if p != nil {
		t.Error("expected nil when not inside a routes/ directory")
	}
}

func TestDetectTanStackRoutes_NoTsxFiles(t *testing.T) {
	tc := makeTcWithNpm("@tanstack/react-router")
	// No .tsx files — only a generated file.
	p := detectTanStackRoutes("src/routes", []string{"routeTree.gen.ts"}, tc)
	if p != nil {
		t.Error("expected nil when no non-generated .tsx files present")
	}
}

func TestDetectTanStackRoutes_MissingNpmPackage(t *testing.T) {
	// npmPackages is set but doesn't include tanstack router.
	tc := makeTcWithNpm("react")
	p := detectTanStackRoutes("src/routes", []string{"index.tsx"}, tc)
	if p != nil {
		t.Error("expected nil when @tanstack/* not in npm deps")
	}
}

func TestDetectTanStackRoutes_FallbackToFrameworkWhenNoLockfile(t *testing.T) {
	// npmPackages is nil (no lockfile) but framework was detected.
	tc := makeTcWithFramework(FrameworkTanStack)
	p := detectTanStackRoutes("src/routes", []string{"index.tsx"}, tc)
	if p == nil {
		t.Fatal("expected detection via framework fallback when npmPackages is nil")
	}
}

func TestDetectTanStackRoutes_NoFrameworkNoLockfile(t *testing.T) {
	tc := makeTcWithFramework(FrameworkNone) // no npm, no framework
	p := detectTanStackRoutes("src/routes", []string{"index.tsx"}, tc)
	if p != nil {
		t.Error("expected nil when neither npm packages nor framework detected")
	}
}

func TestDetectTanStackRoutes_OnlyEmittedAtRoutesRoot(t *testing.T) {
	tc := makeTcWithNpm("@tanstack/react-router")
	// Sub-directory inside routes/ — should NOT emit.
	p := detectTanStackRoutes("src/routes/users", []string{"index.tsx"}, tc)
	if p != nil {
		t.Error("expected nil for sub-directory inside routes/; target should only be at routes/ root")
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
	tc := &tsConfig{isolatedDeclarations: true}
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
	tc := &tsConfig{isolatedDeclarations: true}
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
		},
	}
	patterns := detectCodegen("src", []string{"input.ts"}, tc)
	if len(patterns) != 1 || patterns[0].Name != "my_gen" {
		t.Errorf("expected custom codegen pattern, got %v", patterns)
	}
}

// ---- parseCodegenDirective tests -------------------------------------------

func TestParseCodegenDirective_BasicSingleOut(t *testing.T) {
	cp := parseCodegenDirective("api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}")
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
	cp := parseCodegenDirective("my_gen @npm//:tool_bin types.ts,client.ts generate")
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
	cp := parseCodegenDirective("prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}")
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
	cp := parseCodegenDirective("my_gen @npm//:tool_bin")
	if cp != nil {
		t.Errorf("expected nil for directive with too few fields, got %+v", cp)
	}
}

func TestParseCodegenDirective_EmptyValue(t *testing.T) {
	cp := parseCodegenDirective("")
	if cp != nil {
		t.Errorf("expected nil for empty directive value")
	}
}

func TestParseCodegenDirective_EmptyDirAfterPrefix(t *testing.T) {
	// "dir:" with nothing after it should fail.
	cp := parseCodegenDirective("my_gen @npm//:tool_bin dir: generate")
	if cp != nil {
		t.Errorf("expected nil for empty dir: value")
	}
}

func TestParseCodegenDirective_ArgsOptional(t *testing.T) {
	// No args — only name + generator + outs.
	cp := parseCodegenDirective("gen @npm//:tool_bin output.ts")
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
