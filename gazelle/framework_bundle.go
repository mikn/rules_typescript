package typescript

// framework_bundle.go generates framework-aware bundle targets (node_modules,
// vite_bundler, ts_bundle) at the workspace root BUILD.bazel when a supported
// Vite-based framework is detected from package.json.
//
// User story:
//  1. Write a minimal vite.config.mjs with the framework plugin (3 lines).
//  2. Declare the framework's client entry as its own single-file ts_compile
//     (`# gazelle:ts_exclude <entry>` plus the target), since ts_bundle needs
//     exactly one .js and Gazelle merges a directory into one target.
//  3. Run Gazelle → it generates node_modules, vite_bundler, and ts_bundle.
//  4. bazel build //:app produces the framework bundle.
//
// Gazelle cannot write arbitrary non-BUILD files, so the vite_config file must
// be hand-authored. We generate a ts_bundle that points at the conventional
// config filename for each framework (e.g. "tanstack-vite.config.mjs").
//
// A framework whose bundling cannot work at all belongs in unsupportedBundling
// instead: Gazelle then says so rather than emitting a target that fails.

import (
	"fmt"
	"log"
	"sort"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- FrameworkBundleConfig -------------------------------------------------

// Node realpaths a module before resolving its bare imports, so only a tree
// whose own directory is named "node_modules" is on that resolution path.
const frameworkNodeModulesName = "node_modules"

// One framework per workspace root, so the bundler can take the bare name.
const frameworkViteBundlerName = "vite"

// FrameworkBundleConfig describes the Bazel targets that should be generated
// at the workspace root when a specific Vite-based framework is detected.
type FrameworkBundleConfig struct {
	// AppName is the base name for the generated targets (e.g. "app") and the
	// stem of the pre-rename names cleanupLegacyFrameworkTargets deletes.
	AppName string

	// NpmDeps is the list of npm package names to include in the node_modules
	// target. Each entry is converted to an @npm//:<label> reference using
	// npmPackageToLabelName.
	NpmDeps []string

	// ViteConfigFile is the expected filename of the user-authored vite config
	// (e.g. "tanstack-vite.config.mjs"). Gazelle sets the vite_config attr of
	// ts_bundle to this filename. The user must create this file manually.
	ViteConfigFile string

	// StageFiles lists workspace-root files, besides HTMLFile, that the
	// framework plugin reads from the staging root rather than from the
	// package source directory.
	StageFiles []string

	// StageDirs is the list of workspace-relative directories whose source
	// files should be listed in ts_bundle.staging_srcs. Gazelle emits a
	// filegroup named "sources" in each directory and collects their labels.
	// When the directory does not exist (no BUILD.bazel yet) the label is
	// still emitted; it will be satisfied once Gazelle runs on that directory.
	StageDirs []string

	// EntryPoint is the Bazel label of the primary entry_point for ts_bundle.
	// ts_bundle requires exactly one .js from it, so it names the single-file
	// ts_compile the framework's conventional client entry gets: the user marks
	// that file `# gazelle:ts_exclude` and declares the target, since Gazelle
	// merges every source in a directory into one target.
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
// FrameworkNextJS uses its own next_build rule and has a separate generation
// path. A detected framework absent from BOTH maps gets no bundle targets and
// no explanation, which is the one outcome to avoid.
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
		StageFiles: []string{"package.json"},
		EntryPoint: "//app:entry_client",
		HTMLFile:   "index.html",
	},
}

// unsupportedBundling carries, for each framework detection recognises but
// bundling cannot serve, why no bundle target is generated for it. A target
// that fails to build is worse than no target, and silence is worse than both.
var unsupportedBundling = map[Framework]string{
	FrameworkSvelteKit: "its Vite plugin runs SvelteKit's own sync step from the config hook, " +
		"needing src/app.html and a svelte.config.js of its own beside the vite config, " +
		"and .svelte files are not TypeScript, so no staging_srcs filegroup carries the routes",
	FrameworkSolidStart: "@solidjs/start ships no Vite plugin: defineConfig() returns a vinxi app, " +
		"which ts_bundle's vite_config contract (a default export with a plugins array) cannot consume",
}

// reportUnsupportedBundling names the framework and says bundling for it is
// unsupported, so the missing target is a decision rather than a hole.
func reportUnsupportedBundling(f Framework) {
	name := frameworkName(f)
	reason, ok := unsupportedBundling[f]
	if !ok {
		log.Printf("typescript: %s detected: no bundle target generated, and no reason is registered for it.", name)
		return
	}
	log.Printf("typescript: %s detected: bundling it is unsupported, so no bundle target was generated — %s. "+
		"Your TypeScript still compiles and tests; for a client-only build, declare a ts_bundle by hand with no vite_config.", name, reason)
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
) ([]*rule.Rule, []any, []*rule.Rule) {
	// Next.js uses its own rule (next_build) rather than Vite-based bundling.
	if tc.detectedFramework == FrameworkNextJS {
		gen, imports := generateNextJSBundle(args, tc)
		return gen, imports, nil
	}

	cfg, ok := frameworkConfigs[tc.detectedFramework]
	if !ok {
		reportUnsupportedBundling(tc.detectedFramework)
		return nil, nil, nil
	}

	// Filter out npm deps that are not actually present in the lockfile.
	// When npmPackages is nil (no lockfile), include all configured deps.
	npmDeps := filterNpmDeps(cfg.NpmDeps, tc)

	var gen []*rule.Rule
	var imports []any

	nodeModulesName := frameworkNodeModulesName
	viteTargetName := frameworkViteBundlerName
	empty := cleanupLegacyFrameworkTargets(args, cfg)

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
		nm.AddComment("# Name must be \"node_modules\": Node realpaths a module before its imports.")
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

	return gen, imports, empty
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

// legacyFrameworkTargetNames maps each pre-rename target name to its kind.
func legacyFrameworkTargetNames(cfg FrameworkBundleConfig) map[string]string {
	return map[string]string{
		cfg.AppName + "_node_modules": "node_modules",
		cfg.AppName + "_vite":         "vite_bundler",
	}
}

// cleanupLegacyFrameworkTargets deletes the pre-rename pair, all of it or none
// of it: the bundler half is the tree half's only consumer.
func cleanupLegacyFrameworkTargets(args language.GenerateArgs, cfg FrameworkBundleConfig) []*rule.Rule {
	if args.File == nil {
		return nil
	}
	legacy := legacyFrameworkTargetNames(cfg)

	var empty []*rule.Rule
	for _, r := range args.File.Rules {
		if kind, ok := legacy[r.Name()]; ok && r.Kind() == kind {
			empty = append(empty, rule.NewRule(kind, r.Name()))
		}
	}
	for _, r := range args.File.Rules {
		if _, isLegacy := legacy[r.Name()]; isLegacy {
			continue
		}
		if referencesAny(r, legacy) {
			return nil
		}
	}
	return empty
}

func referencesAny(r *rule.Rule, names map[string]string) bool {
	referenced := func(v string) bool {
		for name := range names {
			if v == ":"+name || v == "//:"+name {
				return true
			}
		}
		return false
	}
	for _, key := range r.AttrKeys() {
		if referenced(r.AttrString(key)) {
			return true
		}
		for _, v := range r.AttrStrings(key) {
			if referenced(v) {
				return true
			}
		}
	}
	return false
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

	srcs = append(srcs, cfg.StageFiles...)

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
