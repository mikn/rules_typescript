package typescript

// framework_bundle.go generates framework-aware bundle targets (node_modules,
// vite_bundler, ts_bundle) at the workspace root BUILD.bazel when a supported
// Vite-based framework is detected from package.json.
//
// User story:
//  1. Write a minimal vite.config.mjs with the framework plugin (3 lines).
//  2. Run Gazelle → it generates node_modules, vite_bundler, and ts_bundle.
//  3. bazel build //:app produces the framework bundle.
//
// Gazelle cannot write arbitrary non-BUILD files, so the vite_config file must
// be hand-authored. We generate a ts_bundle that points at the conventional
// config filename for each framework (e.g. "tanstack-vite.config.mjs").

import (
	"fmt"
	"sort"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- FrameworkBundleConfig -------------------------------------------------

// FrameworkBundleConfig describes the Bazel targets that should be generated
// at the workspace root when a specific Vite-based framework is detected.
type FrameworkBundleConfig struct {
	// AppName is the base name for all generated targets (e.g. "app").
	// node_modules → "<AppName>_node_modules"
	// vite_bundler → "<AppName>_vite"
	// ts_bundle    → "<AppName>"
	AppName string

	// NpmDeps is the list of npm package names to include in the node_modules
	// target. Each entry is converted to an @npm//:<label> reference using
	// npmPackageToLabelName.
	NpmDeps []string

	// ViteConfigFile is the expected filename of the user-authored vite config
	// (e.g. "tanstack-vite.config.mjs"). Gazelle sets the vite_config attr of
	// ts_bundle to this filename. The user must create this file manually.
	ViteConfigFile string

	// StageDirs is the list of workspace-relative directories whose source
	// files should be listed in ts_bundle.staging_srcs. Gazelle emits a
	// filegroup named "sources" in each directory and collects their labels.
	// When the directory does not exist (no BUILD.bazel yet) the label is
	// still emitted; it will be satisfied once Gazelle runs on that directory.
	StageDirs []string

	// EntryPoint is the Bazel label of the primary entry_point for ts_bundle.
	// Example: "//src/app:main" or ":entry_client".
	EntryPoint string

	// HTMLFile is the workspace-relative path to the HTML entry file
	// (typically "index.html"). Used as the html attr of ts_bundle.
	HTMLFile string

	// BundleName is the Bazel target name for the ts_bundle rule.
	// Typically matches AppName (e.g. "app") or a framework-specific name
	// (e.g. "app_remix").
	BundleName string
}

// frameworkConfigs maps each detected Framework to its bundle configuration.
// Only frameworks with Vite-based bundling are included here; FrameworkNextJS
// uses its own next_build rule and has a separate generation path.
var frameworkConfigs = map[Framework]FrameworkBundleConfig{
	FrameworkTanStack: {
		AppName:        "app",
		BundleName:     "app",
		ViteConfigFile: "tanstack-vite.config.mjs",
		NpmDeps: []string{
			"vite",
			"react",
			"react-dom",
			"@tanstack/react-start",
			"@tanstack/react-router",
			"zod",
			"h3",
		},
		StageDirs:  []string{"src/routes", "src/app", "src/lib", "src/components"},
		EntryPoint: "//src/app:main",
		HTMLFile:   "index.html",
	},
	FrameworkRemix: {
		AppName:        "app",
		BundleName:     "app_remix",
		ViteConfigFile: "remix-vite.config.mjs",
		NpmDeps: []string{
			"vite",
			"react",
			"react-dom",
			"@remix-run/dev",
			"@remix-run/react",
			"@remix-run/node",
		},
		StageDirs:  []string{"app/routes", "app"},
		EntryPoint: ":entry_client",
		HTMLFile:   "index.html",
	},
	FrameworkSvelteKit: {
		AppName:        "app",
		BundleName:     "app_sveltekit",
		ViteConfigFile: "svelte.config.mjs",
		NpmDeps: []string{
			"vite",
			"@sveltejs/kit",
			"@sveltejs/vite-plugin-svelte",
		},
		StageDirs:  []string{"src/routes", "src/lib"},
		EntryPoint: "//src:app",
		HTMLFile:   "index.html",
	},
	FrameworkSolidStart: {
		AppName:        "app",
		BundleName:     "app_solid",
		ViteConfigFile: "solid-vite.config.mjs",
		NpmDeps: []string{
			"vite",
			"solid-js",
			"@solidjs/start",
		},
		StageDirs:  []string{"src/routes", "src"},
		EntryPoint: "//src:app",
		HTMLFile:   "index.html",
	},
}

// ---- generation ------------------------------------------------------------

// generateFrameworkBundle generates the root-level bundle targets for the
// detected framework and returns rules ready for inclusion in GenerateResult.
//
// For Vite-based frameworks it generates:
//  1. A node_modules rule with the framework's npm deps.
//  2. A vite_bundler rule pointing at the node_modules target.
//  3. A ts_bundle rule with staging_srcs, vite_config, and entry_point.
//
// For Next.js (FrameworkNextJS) it delegates to generateNextJSBundle which
// generates node_modules and next_build targets.
//
// The function is called from generateRules when rel == "" and a framework is
// detected. It only generates rules that do not already exist in the current
// BUILD file (Gazelle's merge handles updates to existing rules).
//
// staging_srcs uses filegroup labels (//dir:sources) from each stageDir. The
// filegroup targets are generated by generateSourcesFilegroup (called per
// directory) and exported with visibility = ["//visibility:public"].
func generateFrameworkBundle(
	args language.GenerateArgs,
	tc *tsConfig,
) ([]*rule.Rule, []any) {
	// Next.js uses its own rule (next_build) rather than Vite-based bundling.
	if tc.detectedFramework == FrameworkNextJS {
		return generateNextJSBundle(args, tc)
	}

	cfg, ok := frameworkConfigs[tc.detectedFramework]
	if !ok {
		// Framework detected but no bundle config registered.
		return nil, nil
	}

	// Filter out npm deps that are not actually present in the lockfile.
	// When npmPackages is nil (no lockfile), include all configured deps.
	npmDeps := filterNpmDeps(cfg.NpmDeps, tc)

	var gen []*rule.Rule
	var imports []any

	nodeModulesName := cfg.AppName + "_node_modules"
	viteTargetName := cfg.AppName + "_vite"

	// ---- node_modules target -----------------------------------------------
	// Only generate if not already present in the BUILD file.
	if !ruleExists(args, "node_modules", nodeModulesName) {
		nmDeps := make([]string, 0, len(npmDeps))
		for _, pkg := range npmDeps {
			nmDeps = append(nmDeps, npmLabel(pkg))
		}
		sort.Strings(nmDeps)

		nm := rule.NewRule("node_modules", nodeModulesName)
		nm.SetAttr("deps", nmDeps)
		nm.SetAttr("visibility", []string{"//visibility:public"})
		nm.AddComment("# Framework node_modules for " + frameworkName(tc.detectedFramework))
		gen = append(gen, nm)
		imports = append(imports, nil)
	}

	// ---- vite_bundler target -----------------------------------------------
	if !ruleExists(args, "vite_bundler", viteTargetName) {
		vb := rule.NewRule("vite_bundler", viteTargetName)
		vb.SetAttr("vite", "@npm//:vite")
		vb.SetAttr("node_modules", ":"+nodeModulesName)
		gen = append(gen, vb)
		imports = append(imports, nil)
	}

	// ---- ts_bundle target --------------------------------------------------
	if !ruleExists(args, "ts_bundle", cfg.BundleName) {
		tb := rule.NewRule("ts_bundle", cfg.BundleName)
		tb.SetAttr("mode", "app")
		if cfg.HTMLFile != "" {
			tb.SetAttr("html", cfg.HTMLFile)
		}
		tb.SetAttr("entry_point", cfg.EntryPoint)
		tb.SetAttr("bundler", ":"+viteTargetName)
		if cfg.ViteConfigFile != "" {
			tb.SetAttr("vite_config", cfg.ViteConfigFile)
		}
		stagingSrcs := buildStagingSrcs(cfg)
		if len(stagingSrcs) > 0 {
			tb.SetAttr("staging_srcs", stagingSrcs)
		}
		gen = append(gen, tb)
		imports = append(imports, nil)
	}

	return gen, imports
}

// generateNextJSBundle generates root-level targets for a Next.js application.
//
// It generates:
//  1. A node_modules rule with next, react, react-dom and their peers.
//  2. A next_build rule pointing at the node_modules target.
//
// The user must hand-author next.config.mjs (or next.config.js). Gazelle
// generates the Bazel wiring; the Next.js config itself is the user's concern.
func generateNextJSBundle(
	args language.GenerateArgs,
	tc *tsConfig,
) ([]*rule.Rule, []any) {
	var gen []*rule.Rule
	var imports []any

	nextjsNpmDeps := []string{
		"next",
		"react",
		"react-dom",
	}

	// Filter out deps not present in the lockfile (when lockfile is loaded).
	npmDeps := filterNpmDeps(nextjsNpmDeps, tc)

	nodeModulesName := "node_modules"

	// ---- node_modules target -----------------------------------------------
	if !ruleExists(args, "node_modules", nodeModulesName) {
		nmDeps := make([]string, 0, len(npmDeps))
		for _, pkg := range npmDeps {
			nmDeps = append(nmDeps, npmLabel(pkg))
		}
		sort.Strings(nmDeps)

		nm := rule.NewRule("node_modules", nodeModulesName)
		nm.SetAttr("deps", nmDeps)
		nm.SetAttr("visibility", []string{"//visibility:public"})
		nm.AddComment("# Next.js node_modules")
		gen = append(gen, nm)
		imports = append(imports, nil)
	}

	// ---- next_build target -------------------------------------------------
	// Detect the conventional Next.js config filename by checking the repo
	// root for the standard filenames in priority order.
	configFile := "next.config.mjs" // default to the ESM config convention

	if !ruleExists(args, "next_build", "app") {
		nb := rule.NewRule("next_build", "app")
		// srcs uses glob() — emit the srcs attr as a list containing
		// glob expressions for the typical Next.js directory layout.
		nb.SetAttr("srcs", []string{
			globExprPrefix + "[\"app/**/*.tsx\", \"app/**/*.ts\", \"lib/**/*.ts\"])",
		})
		nb.SetAttr("config", configFile)
		nb.SetAttr("node_modules", ":"+nodeModulesName)
		nb.AddComment("# Next.js application build")
		nb.AddComment("# Customize srcs glob to match your project layout.")
		gen = append(gen, nb)
		imports = append(imports, nil)
	}

	return gen, imports
}

// generateSourcesFilegroup generates a "sources" filegroup rule in a
// sub-package directory, exporting all .ts/.tsx source files for use in
// ts_bundle.staging_srcs at the workspace root.
//
// This is called for each directory that appears in a framework's StageDirs
// list, and is returned alongside the normal ts_compile rules for that directory.
// The filegroup only collects non-generated, non-test TypeScript files.
func generateSourcesFilegroup(srcFiles []string) *rule.Rule {
	if len(srcFiles) == 0 {
		return nil
	}
	fg := rule.NewRule("filegroup", "sources")
	sorted := make([]string, len(srcFiles))
	copy(sorted, srcFiles)
	sort.Strings(sorted)
	fg.SetAttr("srcs", sorted)
	fg.SetAttr("visibility", []string{"//visibility:public"})
	return fg
}

// ---- helpers ---------------------------------------------------------------

// filterNpmDeps returns only those package names from pkgs that are present
// in tc.npmPackages. When tc.npmPackages is nil (no lockfile loaded) all
// packages are returned unchanged.
func filterNpmDeps(pkgs []string, tc *tsConfig) []string {
	if tc.npmPackages == nil {
		return pkgs
	}
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if hasNpmPackage(tc, pkg) {
			out = append(out, pkg)
		}
	}
	// Always include "vite" itself even if not found (it should always be present).
	hasVite := false
	for _, p := range out {
		if p == "vite" {
			hasVite = true
			break
		}
	}
	if !hasVite && len(out) == 0 {
		// No packages found at all — fall back to the full list to avoid
		// generating an empty node_modules target.
		return pkgs
	}
	return out
}

// npmLabel converts an npm package name to its @npm//:<label> form.
func npmLabel(pkgName string) string {
	return "@npm//:" + npmPackageToLabelName(pkgName)
}

// frameworkName returns a human-readable string for the framework.
func frameworkName(f Framework) string {
	switch f {
	case FrameworkTanStack:
		return "TanStack Start"
	case FrameworkRemix:
		return "Remix"
	case FrameworkSvelteKit:
		return "SvelteKit"
	case FrameworkSolidStart:
		return "SolidStart"
	case FrameworkNextJS:
		return "Next.js"
	default:
		return "unknown framework"
	}
}

// ruleExists returns true when the BUILD file already contains a rule with the
// given kind and name.
func ruleExists(args language.GenerateArgs, kind, name string) bool {
	if args.File == nil {
		return false
	}
	for _, r := range args.File.Rules {
		if r.Kind() == kind && r.Name() == name {
			return true
		}
	}
	return false
}

// buildStagingSrcs assembles the staging_srcs label list for ts_bundle.
// Each entry in cfg.StageDirs becomes a filegroup label //dir:sources.
// "index.html" (and any other HTMLFile) is prepended as a plain file label
// at the root so the Remix/TanStack wrapper can stage it correctly.
func buildStagingSrcs(cfg FrameworkBundleConfig) []string {
	var srcs []string

	// HTML file at the workspace root is referenced without a target separator.
	if cfg.HTMLFile != "" {
		srcs = append(srcs, cfg.HTMLFile)
	}

	// One filegroup label per stage directory.
	for _, dir := range cfg.StageDirs {
		srcs = append(srcs, fmt.Sprintf("//%s:sources", dir))
	}

	return srcs
}

// isStagedDir returns true when rel is one of the stage directories for the
// detected framework. This is used by generateRules to decide whether to emit
// a filegroup alongside the ts_compile target.
func isStagedDir(rel string, tc *tsConfig) bool {
	cfg, ok := frameworkConfigs[tc.detectedFramework]
	if !ok {
		return false
	}
	for _, d := range cfg.StageDirs {
		if rel == d {
			return true
		}
	}
	return false
}
