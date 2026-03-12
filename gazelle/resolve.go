package typescript

import (
	"log"
	"path"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// ---- Imports (indexer) -----------------------------------------------------

// importsForRule returns the ImportSpecs that can be used to import rule r.
// These are stored in the RuleIndex so that other rules can resolve their deps
// against them.
//
// For ts_compile rules we emit one ImportSpec per "natural" import path that
// TypeScript code would use to reach this target:
//   - The package-relative path of each src file (without extension).
//   - The package-relative directory path (for index.ts based imports).
//
// For css_library, css_module, and asset_library rules we emit one ImportSpec
// per src using the workspace-relative path (e.g. "src/Button.module.css") so
// that TypeScript files that import these paths can be resolved to the correct
// target.
func importsForRule(_ *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	switch r.Kind() {
	case "css_library", "css_module", "asset_library", "json_library":
		pkg := f.Pkg
		var specs []resolve.ImportSpec
		for _, src := range r.AttrStrings("srcs") {
			imp := path.Join(pkg, src)
			specs = append(specs, resolve.ImportSpec{Lang: languageName, Imp: imp})
		}
		return specs
	}

	if r.Kind() != "ts_compile" && r.Kind() != "ts_test" {
		return nil
	}

	pkg := f.Pkg // Bazel package path (same as rel)

	var specs []resolve.ImportSpec

	srcs := r.AttrStrings("srcs")
	for _, src := range srcs {
		// Emit the import path without extension so that
		// both "./Button" and "./Button.tsx" resolve to this rule.
		withoutExt := dropTsExtension(src)
		imp := path.Join(pkg, withoutExt)
		specs = append(specs, resolve.ImportSpec{Lang: languageName, Imp: imp})

		// If the src is index.ts/tsx, also emit the parent directory path so
		// that "import from './components'" resolves to this rule.
		if isIndexFile(src) {
			specs = append(specs, resolve.ImportSpec{Lang: languageName, Imp: pkg})
		}
	}

	return specs
}

// ---- Resolve (dep resolver) ------------------------------------------------

// resolveImports converts raw import strings (stored in GenerateResult.Imports)
// into Bazel deps on rule r.
func resolveImports(
	c *config.Config,
	ix *resolve.RuleIndex,
	r *rule.Rule,
	importsIface any,
	from label.Label,
) {
	if importsIface == nil {
		return
	}
	imports, ok := importsIface.([]string)
	if !ok || len(imports) == 0 {
		return
	}

	tc := getConfig(c)

	var deps []string
	seen := make(map[string]struct{})

	addDep := func(dep string) {
		if _, dup := seen[dep]; !dup {
			seen[dep] = struct{}{}
			deps = append(deps, dep)
		}
	}

	for _, imp := range imports {
		resolved := resolveImport(c, ix, tc, imp, from)
		if resolved == "" {
			if tc.warnUnresolved && !isNodeBuiltin(imp) {
				log.Printf("gazelle: WARNING: unresolved import %q in //%s:%s (tried: relative, path-alias, npm)", imp, from.Pkg, from.Name)
			}
			continue
		}
		addDep(resolved)
	}

	// For ts_test targets, append any runtimeDeps.test labels from
	// gazelle_ts.json. These are already valid Bazel labels (e.g.
	// "@npm//:happy-dom") for packages needed at test runtime that are
	// never statically imported — happy-dom, @vitest/coverage-v8, react
	// (JSX runtime), etc.
	if r.Kind() == "ts_test" {
		for _, lbl := range tc.runtimeDepsTest {
			addDep(lbl)
		}
	}

	if len(deps) == 0 {
		return
	}

	sort.Strings(deps)
	r.SetAttr("deps", deps)
}

// resolveImport attempts to resolve a single import specifier to a Bazel label
// string. Returns "" if the import cannot be resolved and should be skipped.
func resolveImport(
	c *config.Config,
	ix *resolve.RuleIndex,
	tc *tsConfig,
	imp string,
	from label.Label,
) string {
	switch {
	case isRelativeImport(imp):
		return resolveRelative(c, ix, imp, from)
	case isPathAlias(tc, imp):
		return resolvePathAlias(c, ix, tc, imp, from)
	default:
		return resolveNpmPackage(tc, imp)
	}
}

// ---- relative import resolution --------------------------------------------

// isRelativeImport returns true if the specifier starts with "./" or "../".
func isRelativeImport(imp string) bool {
	return strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../")
}

// resolveRelative resolves a relative import specifier (e.g. "./utils") to a
// Bazel label. It tries, in order:
//  1. Exact file match in the same Bazel package.
//  2. Directory with index.ts → sibling package.
//  3. Rule index lookup using the computed workspace-relative path.
func resolveRelative(
	_ *config.Config,
	ix *resolve.RuleIndex,
	imp string,
	from label.Label,
) string {
	// Compute the workspace-relative path for the import target.
	// from.Pkg is the package directory (rel), e.g. "src/components/button".
	targetRel := path.Clean(path.Join(from.Pkg, imp))

	// 1. Try the exact path as-is in the index.
	if lbl, selfImport := lookupInIndex(ix, targetRel, from); lbl != "" {
		return lbl
	} else if selfImport {
		// Import resolves to the same package → no dep needed.
		return ""
	}

	// 2. Try common extensions if not already present.
	for _, ext := range []string{".ts", ".tsx", ".js", ".json", ".module.css", ".css"} {
		if lbl, selfImport := lookupInIndex(ix, targetRel+ext, from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}
	}

	// 3. Try treating it as a directory import (index.ts convention).
	for _, ext := range []string{".ts", ".tsx"} {
		if lbl, selfImport := lookupInIndex(ix, path.Join(targetRel, "index")+ext, from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}
	}

	// 4. Fall back to constructing a label from the path, using the directory
	//    basename as the target name.
	return labelFromRel(targetRel)
}

// lookupInIndex searches the RuleIndex for a ts_compile rule that exports the
// given workspace-relative import path. Returns the label string (relativized
// to the current repo) and a boolean indicating whether the result was a
// self-import that was skipped. The two return values are mutually exclusive:
// if lbl != "", selfImport is false; if selfImport is true, lbl is "".
func lookupInIndex(ix *resolve.RuleIndex, impPath string, from label.Label) (string, bool) {
	results := ix.FindRulesByImport(resolve.ImportSpec{
		Lang: languageName,
		Imp:  impPath,
	}, languageName)

	for _, r := range results {
		if r.IsSelfImport(from) {
			// The import resolves to the rule that contains it — same package.
			return "", true
		}
		// Relativize the label: strip the repo prefix when it refers to the
		// same repository so we emit "//pkg:name" instead of "@repo//pkg:name".
		lbl := r.Label.Rel(from.Repo, from.Pkg)
		return lbl.String(), false
	}
	return "", false
}

// labelFromRel constructs a best-effort Bazel label from a workspace-relative
// path when the rule index doesn't have an entry. The target name is the
// basename of the path, consistent with the naming convention used in
// targetNameForDir.
func labelFromRel(rel string) string {
	// Drop any file extension.
	rel = dropTsExtension(rel)
	// Handle index file: use the directory as the target.
	if path.Base(rel) == "index" {
		rel = path.Dir(rel)
	}
	pkg := rel
	name := path.Base(pkg)
	if pkg == "" || pkg == "." {
		return ""
	}
	lbl := label.New("", pkg, name)
	return lbl.String()
}

// ---- path alias resolution -------------------------------------------------

// isPathAlias returns true if the import matches any configured path alias
// prefix.
func isPathAlias(tc *tsConfig, imp string) bool {
	for prefix := range tc.pathAliases {
		if strings.HasPrefix(imp, prefix) {
			return true
		}
	}
	return false
}

// resolvePathAlias expands a path alias import to a workspace-relative path,
// then delegates to the index / label construction.
//
// Resolution order for an alias like "@/utils/helpers" (alias "@/" → "src/"):
//  1. Exact path:          src/utils/helpers
//  2. With extension:      src/utils/helpers.ts / .tsx / .js
//  3. Index file:          src/utils/helpers/index(.ts/.tsx)
//  4. Parent directory:    src/utils (handles non-barrel sub-path imports that
//                          point to files compiled into the parent package)
//  5. labelFromRel fallback for when the target hasn't been indexed yet.
func resolvePathAlias(
	_ *config.Config,
	ix *resolve.RuleIndex,
	tc *tsConfig,
	imp string,
	from label.Label,
) string {
	for prefix, dir := range tc.pathAliases {
		if !strings.HasPrefix(imp, prefix) {
			continue
		}
		rest := imp[len(prefix):]
		// dir is workspace-relative, e.g. "src/"
		dir = strings.TrimSuffix(dir, "/")
		targetRel := path.Join(dir, rest)

		if lbl, selfImport := lookupInIndex(ix, targetRel, from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}
		for _, ext := range []string{".ts", ".tsx", ".js"} {
			if lbl, selfImport := lookupInIndex(ix, targetRel+ext, from); lbl != "" {
				return lbl
			} else if selfImport {
				return ""
			}
		}
		// Try index file convention.
		for _, ext := range []string{".ts", ".tsx"} {
			if lbl, selfImport := lookupInIndex(ix, path.Join(targetRel, "index")+ext, from); lbl != "" {
				return lbl
			} else if selfImport {
				return ""
			}
		}
		// Fallback: legacy bare index lookup (no extension).
		if lbl, selfImport := lookupInIndex(ix, path.Join(targetRel, "index"), from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}

		// Sub-path fallback: "@/utils/helpers" might refer to a file compiled
		// into the parent package (//src/utils:utils) rather than a dedicated
		// sub-package.  Walk up one directory and try to find a target there.
		// Example: "@/utils/helpers" → "src/utils/helpers" (miss) →
		//          try "src/utils" → found //src/utils:utils → return it.
		parent := path.Dir(targetRel)
		if parent != "." && parent != targetRel {
			if lbl, selfImport := lookupInIndex(ix, parent, from); lbl != "" {
				return lbl
			} else if selfImport {
				return ""
			}
		}

		return labelFromRel(targetRel)
	}
	return ""
}

// ---- npm package resolution ------------------------------------------------

// resolveNpmPackage maps a bare specifier (e.g. "react", "@tanstack/router")
// to a Bazel label. It first checks the npm package mapping (if present),
// then falls back to the default @npm//:target-name convention.
//
// The label format matches what rules_typescript's npm_translate_lock generates:
//   - "vitest"             → "@npm//:vitest"
//   - "@types/react"       → "@npm//:types_react"
//   - "@tanstack/router"   → "@npm//:tanstack_router"
func resolveNpmPackage(tc *tsConfig, imp string) string {
	// Skip Node.js built-in modules (e.g. "node:fs", "node:path").
	if strings.HasPrefix(imp, "node:") {
		return ""
	}

	// Bare specifiers must not contain a leading dot or slash.
	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "/") {
		return ""
	}

	// Strip sub-path imports: "react/something" → package is "react".
	// Scoped packages: "@scope/pkg/sub" → package is "@scope/pkg".
	pkgName := barePackageName(imp)

	// Lookup in the explicit npm mapping first.
	if tc.npmPackages != nil {
		if lbl, ok := tc.npmPackages[pkgName]; ok {
			return lbl
		}
	}

	// Default convention: @npm//:target-name.
	// The target name is derived from the npm package name by dropping the
	// leading "@" for scoped packages and replacing "/" with "_", which
	// matches the _package_name_to_label function in npm_translate_lock.bzl.
	targetName := npmPackageToLabelName(pkgName)
	return "@npm//:" + targetName
}

// npmPackageToLabelName converts an npm package name to a Bazel label name
// component, matching the logic in rules_typescript's npm_translate_lock.bzl.
//
// Examples:
//
//	"vitest"          → "vitest"
//	"@types/react"    → "types_react"
//	"@tanstack/router" → "tanstack_router"
func npmPackageToLabelName(pkgName string) string {
	name := pkgName
	if strings.HasPrefix(name, "@") {
		name = name[1:] // drop the leading "@"
	}
	name = strings.ReplaceAll(name, "/", "_")
	return name
}

// barePackageName extracts the npm package name from an import specifier,
// handling scoped packages correctly.
//
// Examples:
//
//	"react"              → "react"
//	"react/jsx-runtime"  → "react"
//	"@tanstack/router"   → "@tanstack/router"
//	"@tanstack/router/history" → "@tanstack/router"
func barePackageName(imp string) string {
	if strings.HasPrefix(imp, "@") {
		// Scoped package: keep the first two path segments.
		parts := strings.SplitN(imp[1:], "/", 3)
		if len(parts) >= 2 {
			return "@" + parts[0] + "/" + parts[1]
		}
		return imp
	}
	// Unscoped: keep the first path segment.
	return strings.SplitN(imp, "/", 2)[0]
}

// ---- built-in module helpers -----------------------------------------------

// isNodeBuiltin returns true for Node.js built-in module specifiers such as
// "node:fs", "node:path", "fs", "path", "os", etc. These are never resolvable
// to a Bazel label so they should not produce a warning even when
// warnUnresolved is enabled.
func isNodeBuiltin(imp string) bool {
	if strings.HasPrefix(imp, "node:") {
		return true
	}
	// Well-known Node.js built-in names (without the node: prefix).
	switch imp {
	case "assert", "async_hooks", "buffer", "child_process", "cluster",
		"console", "constants", "crypto", "dgram", "diagnostics_channel",
		"dns", "domain", "events", "fs", "http", "http2", "https",
		"inspector", "module", "net", "os", "path", "perf_hooks",
		"process", "punycode", "querystring", "readline", "repl",
		"stream", "string_decoder", "timers", "tls", "trace_events",
		"tty", "url", "util", "v8", "vm", "wasi", "worker_threads", "zlib":
		return true
	}
	return false
}

// ---- extension helpers -----------------------------------------------------

// dropTsExtension removes .ts or .tsx from a file path if present.
func dropTsExtension(name string) string {
	for _, ext := range []string{".tsx", ".ts", ".js"} {
		if strings.HasSuffix(name, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

