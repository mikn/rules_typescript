package typescript

// codegen.go implements auto-detection of code generation patterns for
// well-known tools (Prisma, GraphQL Codegen, OpenAPI).
//
// TanStack Router is deliberately absent: its route tree is written by the
// Start Vite plugin during the bundle, into the writable staging directory
// ts_bundle hands it, so a second generator in bazel-bin only drifts from it.
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
	"sort"
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

	// Dir is the directory the # gazelle:ts_codegen directive that declared
	// this pattern was written in. Detectors leave it empty and are matched
	// per directory instead; a directive is inherited by every child directory,
	// and one target belongs in one package.
	Dir string
}

// codegenSrcsPrefix marks the optional srcs field of a ts_codegen directive.
const codegenSrcsPrefix = "srcs:"

// codegenCompileName is the ts_compile that makes a pattern's output
// importable. A target of its own and not the package's: under the default
// declarations = "tsgo" emit, one declaration emit has one rootDir, so a target
// mixing checked-in and generated sources fails at analysis. Switching the
// package to oxc lifts that, at the cost of the emitter its hand-written
// sources use.
func codegenCompileName(p CodegenPattern) (string, bool) {
	for _, out := range p.Outs {
		if isTypeScriptFile(out) {
			return p.Name + "_compile", true
		}
	}
	return "", false
}

// codegenTargetNames returns every target name the given patterns occupy.
func codegenTargetNames(patterns []CodegenPattern) []string {
	var names []string
	for _, p := range patterns {
		names = append(names, p.Name)
		if compile, ok := codegenCompileName(p); ok {
			names = append(names, compile)
		}
	}
	sort.Strings(names)
	return names
}

// codegenOuts returns every file the given patterns declare.
func codegenOuts(patterns []CodegenPattern) []string {
	var outs []string
	for _, p := range patterns {
		outs = append(outs, p.Outs...)
	}
	return outs
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

	// 1. Prisma client generation.
	if p := detectPrisma(fs, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 2. GraphQL codegen.
	if p := detectGraphQLCodegen(files, fs, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 3. OpenAPI / Swagger.
	if p := detectOpenAPI(fs, tc); p != nil {
		patterns = append(patterns, *p)
	}

	// 4. Custom generators from # gazelle:ts_codegen directives, each in the
	// one directory it was declared in.
	for _, custom := range tc.customCodegens {
		if custom.Dir == rel {
			patterns = append(patterns, custom)
		}
	}

	return patterns
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
