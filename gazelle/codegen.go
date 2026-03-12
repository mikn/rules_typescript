package typescript

// codegen.go implements auto-detection of code generation patterns for
// well-known tools (TanStack Router, Prisma, GraphQL Codegen, OpenAPI).
//
// Detection works at the directory level: detectCodegen scans the file list
// and the npm dependency set and emits one CodegenPattern per recognised tool.
// The patterns are converted into ts_codegen rules in generate.go.
//
// Each detector follows a two-step check:
//  1. File presence (e.g. schema.prisma, *.graphql, openapi.yaml).
//  2. npm package presence (e.g. "prisma", "@graphql-codegen/cli").
//
// Both conditions must be true for a pattern to be emitted. This avoids false
// positives in repos that share a monorepo package.json.

import (
	"path"
	"strings"
)

// ---- CodegenPattern --------------------------------------------------------

// CodegenPattern describes a detected code generation opportunity that should
// be emitted as a ts_codegen Bazel target.
type CodegenPattern struct {
	// Name is the Bazel target name (e.g. "route_tree", "prisma_client").
	Name string

	// Srcs is the list of source file globs or explicit file names to pass as
	// the srcs attribute. When a single glob is needed use "glob(...)" syntax
	// so it is emitted verbatim into the BUILD file.
	Srcs []string

	// Outs is the list of declared output file names relative to the package.
	Outs []string

	// OutDir is a directory name to use as the single declared output when
	// the generator produces a directory tree (e.g. Prisma).
	// Mutually exclusive with Outs.
	OutDir string

	// Generator is the Bazel label of the generator executable.
	Generator string

	// Args is the list of command-line arguments passed to the generator.
	Args []string

	// NodeModules controls whether a node_modules attr is emitted referencing
	// the :node_modules target in the same package.
	NodeModules bool

	// Comment is an optional human-readable explanation added to the rule
	// as a BUILD file comment.
	Comment string
}

// ---- npm package helpers ---------------------------------------------------

// hasNpmPackage returns true when pkgName appears in the npm package map held
// in tc.npmPackages. This is the authoritative check when an npm mapping file
// (or pnpm lockfile) was loaded. When the map is nil (no lockfile loaded) we
// fall back to returning false — the caller must decide how to handle that.
func hasNpmPackage(tc *tsConfig, pkgName string) bool {
	if tc.npmPackages == nil {
		return false
	}
	_, ok := tc.npmPackages[pkgName]
	return ok
}

// hasAnyNpmPackage returns true when at least one of the given package names
// is present in tc.npmPackages.
func hasAnyNpmPackage(tc *tsConfig, pkgs ...string) bool {
	for _, pkg := range pkgs {
		if hasNpmPackage(tc, pkg) {
			return true
		}
	}
	return false
}

// npmBinLabel converts an npm package name to the conventional @npm//:bin
// label used for CLI binaries, e.g. "prisma" → "@npm//:prisma_bin".
func npmBinLabel(pkgName string) string {
	return "@npm//:" + npmPackageToLabelName(pkgName) + "_bin"
}

// ---- file set helpers ------------------------------------------------------

// fileSet builds a fast-lookup map from a file list.
func fileSet(files []string) map[string]bool {
	m := make(map[string]bool, len(files))
	for _, f := range files {
		m[f] = true
	}
	return m
}

// hasTsxFiles returns true when the file list contains at least one .tsx file
// that is not a generated file.
func hasTsxFiles(files []string) bool {
	for _, f := range files {
		if strings.HasSuffix(f, ".tsx") && !isGeneratedFile(f) {
			return true
		}
	}
	return false
}

// hasGraphQLFiles returns true when the file list contains at least one
// .graphql or .gql file.
func hasGraphQLFiles(files []string) bool {
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f))
		if ext == ".graphql" || ext == ".gql" {
			return true
		}
	}
	return false
}

// filterGraphQLFiles returns only the .graphql / .gql file names from files.
func filterGraphQLFiles(files []string) []string {
	var out []string
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f))
		if ext == ".graphql" || ext == ".gql" {
			out = append(out, f)
		}
	}
	return out
}

// hasCodegenConfig returns true when files contains codegen.yml, codegen.yaml,
// or codegen.ts (GraphQL codegen config file names).
func hasCodegenConfig(fs map[string]bool) bool {
	return fs["codegen.yml"] || fs["codegen.yaml"] || fs["codegen.ts"] || fs["codegen.json"]
}

// openAPIFileNames returns the first openapi/swagger spec file found in files,
// along with a normalised extension. Returns ("", "") when none found.
func openAPIFileName(fs map[string]bool) string {
	for _, candidate := range []string{
		"openapi.yaml", "openapi.yml", "openapi.json",
		"swagger.yaml", "swagger.yml", "swagger.json",
	} {
		if fs[candidate] {
			return candidate
		}
	}
	return ""
}

// ---- master detector -------------------------------------------------------

// detectCodegen scans a directory for known codegen patterns and returns one
// CodegenPattern per recognised tool. The returned slice is empty when no
// patterns are found.
//
// Parameters:
//
//	rel        workspace-relative directory path (args.Rel)
//	files      all regular files in the directory (args.RegularFiles)
//	tc         per-directory tsConfig (provides npmPackages and customCodegens)
func detectCodegen(rel string, files []string, tc *tsConfig) []CodegenPattern {
	var patterns []CodegenPattern

	fs := fileSet(files)

	// 1. TanStack Router / TanStack Start route generation.
	if p := detectTanStackRoutes(rel, files, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 2. Prisma client generation.
	if p := detectPrisma(fs, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 3. GraphQL codegen.
	if p := detectGraphQLCodegen(files, fs, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 4. OpenAPI / Swagger.
	if p := detectOpenAPI(fs, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 5. Custom generators from # gazelle:ts_codegen directives.
	for _, custom := range tc.customCodegens {
		patterns = append(patterns, custom)
	}

	return patterns
}

// ---- detector: TanStack Router routes --------------------------------------

// detectTanStackRoutes detects the TanStack Router route generation pattern.
//
// Trigger conditions (all must be true):
//  1. The directory path contains a "routes" component.
//  2. @tanstack/react-router or @tanstack/react-start is in npm deps.
//  3. The directory contains at least one non-generated .tsx file.
//
// When npmPackages is nil (no lockfile loaded) we rely on the already-detected
// framework field in tc to avoid the false-positive risk.
func detectTanStackRoutes(rel string, files []string, tc *tsConfig) *CodegenPattern {
	// Condition 1: must be inside a routes/ directory.
	if !isInsideRoutesSegment(rel) {
		return nil
	}

	// Condition 2: npm dependency check.
	// When a package map is available, check explicitly.
	// Fall back to the detectedFramework heuristic when no map is available.
	if tc.npmPackages != nil {
		if !hasAnyNpmPackage(tc,
			"@tanstack/react-router",
			"@tanstack/react-start",
			"@tanstack/start",
		) {
			return nil
		}
	} else if tc.detectedFramework != FrameworkTanStack {
		// No lockfile and no detected framework — skip.
		return nil
	}

	// Condition 3: must have at least one non-generated .tsx route file.
	if !hasTsxFiles(files) {
		return nil
	}

	// Only emit this target once, at the routes/ root directory.
	// Child route subdirectories are handled by the TanStack plugin via
	// AdjustGenerateResult; they do not need a separate ts_codegen target.
	// We identify the routes/ root as the directory whose last path component
	// is "routes" OR whose parent's last component is "routes" and has no
	// further routes/ ancestor (depth == 1 inside routes/).
	if !isRoutesRoot(rel) {
		return nil
	}

	return &CodegenPattern{
		Name:        "route_tree",
		Srcs:        []string{"glob([\"*.tsx\"])"},
		Outs:        []string{"routeTree.gen.ts"},
		Generator:   "@rules_typescript//tools/codegen:tanstack_routes",
		Args:        []string{"--routesDirectory", "{srcs_dir}", "--generatedRouteTree", "{out}"},
		NodeModules: true,
		Comment:     "# TanStack Router: generated route tree from .tsx route files",
	}
}

// isInsideRoutesSegment returns true when any path component of rel equals
// "routes".
func isInsideRoutesSegment(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "routes" {
			return true
		}
	}
	return false
}

// isRoutesRoot returns true when rel ends with the "routes" segment, meaning
// this directory IS the routes/ root (not a sub-directory inside it).
func isRoutesRoot(rel string) bool {
	return path.Base(rel) == "routes"
}

// ---- detector: Prisma ------------------------------------------------------

// detectPrisma detects the Prisma client generation pattern.
//
// Trigger conditions (all must be true):
//  1. schema.prisma exists in the current directory.
//  2. "prisma" is in npm deps (or npmPackages is nil and schema.prisma exists).
func detectPrisma(fs map[string]bool, tc *tsConfig) *CodegenPattern {
	if !fs["schema.prisma"] {
		return nil
	}

	// npm dependency check: require "prisma" or "@prisma/client".
	if tc.npmPackages != nil {
		if !hasAnyNpmPackage(tc, "prisma", "@prisma/client") {
			return nil
		}
	}
	// When npmPackages is nil: presence of schema.prisma is strong enough
	// to emit the target (user can remove it if unwanted).

	return &CodegenPattern{
		Name:        "prisma_client",
		Srcs:        []string{"schema.prisma"},
		OutDir:      "generated/client",
		Generator:   npmBinLabel("prisma"),
		Args:        []string{"generate", "--schema", "{srcs}"},
		NodeModules: true,
		Comment:     "# Prisma: generated client from schema.prisma (produces directory tree)",
	}
}

// ---- detector: GraphQL Codegen ---------------------------------------------

// detectGraphQLCodegen detects the GraphQL Code Generator pattern.
//
// Trigger conditions (all must be true):
//  1. At least one .graphql or .gql file exists.
//  2. A codegen.yml / codegen.yaml / codegen.ts / codegen.json config exists.
//  3. "@graphql-codegen/cli" is in npm deps (or npmPackages is nil).
func detectGraphQLCodegen(files []string, fs map[string]bool, tc *tsConfig) *CodegenPattern {
	if !hasGraphQLFiles(files) {
		return nil
	}
	if !hasCodegenConfig(fs) {
		return nil
	}

	// npm dependency check.
	if tc.npmPackages != nil {
		if !hasAnyNpmPackage(tc, "@graphql-codegen/cli") {
			return nil
		}
	}

	// Determine the config file name.
	configFile := ""
	for _, name := range []string{"codegen.ts", "codegen.yml", "codegen.yaml", "codegen.json"} {
		if fs[name] {
			configFile = name
			break
		}
	}

	// Collect the source file list: graphql files + config.
	graphqlFiles := filterGraphQLFiles(files)
	srcs := append(graphqlFiles, configFile)

	return &CodegenPattern{
		Name:        "graphql_types",
		Srcs:        srcs,
		Outs:        []string{"generated/graphql.ts"},
		Generator:   npmBinLabel("@graphql-codegen/cli"),
		Args:        []string{"--config", configFile},
		NodeModules: true,
		Comment:     "# GraphQL Codegen: generated TypeScript types from .graphql schema",
	}
}

// ---- detector: OpenAPI / Swagger -------------------------------------------

// detectOpenAPI detects the openapi-typescript schema generation pattern.
//
// Trigger conditions (all must be true):
//  1. openapi.yaml / openapi.yml / openapi.json / swagger.yaml / swagger.yml /
//     swagger.json exists in the current directory.
//  2. "openapi-typescript" is in npm deps (or npmPackages is nil and spec present).
func detectOpenAPI(fs map[string]bool, tc *tsConfig) *CodegenPattern {
	specFile := openAPIFileName(fs)
	if specFile == "" {
		return nil
	}

	// npm dependency check.
	if tc.npmPackages != nil {
		if !hasAnyNpmPackage(tc, "openapi-typescript") {
			return nil
		}
	}

	return &CodegenPattern{
		Name:        "api_types",
		Srcs:        []string{specFile},
		Outs:        []string{"api-types.ts"},
		Generator:   npmBinLabel("openapi-typescript"),
		Args:        []string{"{srcs}", "-o", "{out}"},
		NodeModules: false,
		Comment:     "# openapi-typescript: generated TypeScript types from OpenAPI spec",
	}
}
