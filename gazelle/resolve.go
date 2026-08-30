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
		// A ts_codegen label in srcs: its outs are modules of this target, and
		// this target is the only label an importer can depend on -- ts_compile
		// deps take JsInfo, which ts_codegen does not return.
		if isLabelSrc(src) {
			for _, out := range codegenOutsOf(f, strings.TrimPrefix(src, ":")) {
				specs = append(specs, resolve.ImportSpec{
					Lang: languageName,
					Imp:  path.Join(pkg, dropTsExtension(out)),
				})
				if isIndexFile(path.Base(out)) {
					specs = append(specs, resolve.ImportSpec{
						Lang: languageName,
						Imp:  path.Join(pkg, path.Dir(out)),
					})
				}
			}
			continue
		}

		// Emit the import path without extension so that
		// both "./Button" and "./Button.tsx" resolve to this rule.
		withoutExt := dropTsExtension(src)
		imp := path.Join(pkg, withoutExt)
		specs = append(specs, resolve.ImportSpec{Lang: languageName, Imp: imp})

		// If the src is index.ts/tsx, also emit the directory it sits in so
		// that "import from './components'" resolves to this rule. That is the
		// package itself only for a src directly in it: a rolled-up
		// src/index.ts answers to `../src`, not to the package above it.
		if isIndexFile(src) {
			specs = append(specs, resolve.ImportSpec{Lang: languageName, Imp: path.Dir(imp)})
		}
	}

	// A workspace link's package name: no path key covers it, and an unindexed
	// bare specifier is indistinguishable from an npm package.
	if moduleName := r.AttrString("module_name"); moduleName != "" {
		specs = append(specs, resolve.ImportSpec{Lang: languageName, Imp: moduleName})
		for _, src := range srcs {
			if isIndexFile(src) || isLabelSrc(src) {
				continue
			}
			specs = append(specs, resolve.ImportSpec{
				Lang: languageName,
				Imp:  path.Join(moduleName, dropTsExtension(src)),
			})
		}
	}

	return specs
}

// isLabelSrc reports whether a srcs entry names a target rather than a file.
func isLabelSrc(src string) bool {
	return strings.HasPrefix(src, ":") || strings.HasPrefix(src, "//") || strings.HasPrefix(src, "@")
}

// codegenOutsOf returns the outs of the ts_codegen named name in f.
func codegenOutsOf(f *rule.File, name string) []string {
	if f == nil {
		return nil
	}
	for _, r := range f.Rules {
		if r.Kind() == "ts_codegen" && r.Name() == name {
			return r.AttrStrings("outs")
		}
	}
	return nil
}

// The kinds whose sources tsgo type-checks, which is what makes an ambient
// declaration reach them.
var ambientTypesKinds = map[string]bool{"ts_compile": true, "ts_test": true}

func asImports(importsIface any) ([]string, bool) {
	if importsIface == nil {
		return nil, false
	}
	imports, ok := importsIface.([]string)
	if !ok || len(imports) == 0 {
		return nil, false
	}
	return imports, true
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
	tc := getConfig(c)

	var deps []string
	seen := make(map[string]struct{})

	addDep := func(dep string) {
		if _, dup := seen[dep]; !dup {
			seen[dep] = struct{}{}
			deps = append(deps, dep)
		}
	}

	// Ambient declarations have no import to infer a dep from, so they are the
	// one thing this resolver cannot derive and Gazelle cannot repair. A file
	// using only `process` has no imports at all, hence before the guard below.
	if ambientTypesKinds[r.Kind()] {
		for _, lbl := range tc.ambientTypes {
			addDep(lbl)
		}
		for _, lbl := range tc.tsconfigAmbientTypes {
			addDep(lbl)
		}
	}

	imports, ok := asImports(importsIface)
	if !ok {
		if len(deps) > 0 {
			sort.Strings(deps)
			r.SetAttr("deps", deps)
		}
		return
	}

	for _, imp := range imports {
		resolved := resolveImport(c, ix, tc, imp, from)
		if resolved == "" {
			if tc.warnUnresolved && !isNodeBuiltin(barePackageName(imp)) {
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
	imp = dropBundlerQuery(imp)
	switch {
	case isRelativeImport(imp):
		return resolveRelative(c, ix, imp, from)
	case isPathAlias(tc, imp):
		return resolvePathAlias(c, ix, tc, imp, from)
	default:
		// A module_name before an npm package: the hub has no such package.
		if lbl, selfImport := lookupInIndex(ix, imp, from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}
		return resolveNpmPackage(tc, imp)
	}
}

// dropBundlerQuery removes a bundler's query suffix from a specifier.
//
// `./config.json?raw`, `./icon.svg?url` and `./thread?worker` each name the
// same file as the specifier without the suffix; the query only tells the
// bundler how to load it. Left on, it reaches the label, and //pkg/file.json?raw
// is a package that can never exist -- so one of these fails analysis for the
// whole build rather than dropping a single dep.
func dropBundlerQuery(imp string) string {
	if i := strings.IndexByte(imp, '?'); i >= 0 {
		return imp[:i]
	}
	return imp
}

// ---- relative import resolution --------------------------------------------

// isRelativeImport returns true if the specifier starts with "./" or "../".
func isRelativeImport(imp string) bool {
	return strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../")
}

// resolveRelative resolves a relative import specifier (e.g. "./utils") to a
// Bazel label, by asking the rule index for every module path the specifier
// could name and falling back to a constructed label when none is indexed.
func resolveRelative(
	_ *config.Config,
	ix *resolve.RuleIndex,
	imp string,
	from label.Label,
) string {
	// from.Pkg is the package directory (rel), e.g. "src/components/button".
	targetRel := path.Clean(path.Join(from.Pkg, imp))

	for _, key := range moduleIndexKeys(targetRel, relativeImportExtensions) {
		if lbl, selfImport := lookupInIndex(ix, key, from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}
	}

	return labelForUnindexed(targetRel, from)
}

// relativeImportExtensions are the source extensions a relative specifier may
// have omitted, in the order TypeScript tries them.
var relativeImportExtensions = []string{".ts", ".tsx", ".js", ".json", ".module.css", ".css"}

// moduleIndexKeys returns the import paths the module at targetRel could be
// indexed under, in TypeScript's resolution order.
//
// Dropping the extension is what resolves a specifier that spells one out --
// "./x.js" for x.ts, "./x.ts" under allowImportingTsExtensions -- since
// importsForRule indexes a .ts/.tsx/.js src without it. The strict-deps checker
// in ts/private/ts_compile.bzl drops it too; only one of them doing so leaves a
// dep this tool cannot generate and the build rejects.
func moduleIndexKeys(targetRel string, extensions []string) []string {
	bare := dropTsExtension(targetRel)

	keys := []string{targetRel}
	if bare != targetRel {
		keys = append(keys, bare)
	}
	for _, ext := range extensions {
		keys = append(keys, bare+ext)
	}
	for _, ext := range []string{".ts", ".tsx"} {
		keys = append(keys, path.Join(bare, "index")+ext)
	}
	return keys
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

// labelForUnindexed names the package that would have to provide the module at
// rel, for when no indexed rule does.
//
// The package is the module's DIRECTORY: a file path read as a directory leaves
// "no such package", which reads as a defect here rather than a missing target.
func labelForUnindexed(rel string, from label.Label) string {
	pkg := path.Clean(rel)
	if namesAFile(pkg) {
		pkg = path.Dir(pkg)
	}
	if pkg == "" || pkg == "." || pkg == "/" || pkg == ".." || strings.HasPrefix(pkg, "../") {
		return ""
	}
	// A module in the importing package that no rule claims: a label on our own
	// package would be a cycle, and the file is nowhere else.
	if pkg == from.Pkg {
		return ""
	}
	return label.New("", pkg, path.Base(pkg)).String()
}

// namesAFile reports whether rel's last segment is a file. Only an extension
// this extension classifies counts -- a dot in a directory name is not one.
func namesAFile(rel string) bool {
	base := path.Base(rel)
	if base == "index" {
		return true
	}
	return dropTsExtension(base) != base ||
		isCSSFile(base) || isJSONFile(base) || isAssetFile(base)
}

// ---- path alias resolution -------------------------------------------------

// aliasMatch is the alias applied to one import specifier: the key that
// matched, the workspace-relative directory it maps to, and the leftover.
type aliasMatch struct {
	prefix string
	dir    string
	rest   string
}

// matchPathAlias picks the alias entry that resolves imp, if any.
//
// Longest matching key wins: TypeScript's own rule for compilerOptions.paths
// (findBestPatternMatch scores candidates by prefix length), under which a
// wildcard-free key spanning the whole specifier always beats a wildcard one.
// Comparing keys is also what keeps the winner off Go's map iteration order.
func matchPathAlias(tc *tsConfig, imp string) (aliasMatch, bool) {
	var best aliasMatch
	found := false
	for prefix, dir := range tc.pathAliases {
		rest, ok := aliasRest(prefix, imp)
		if !ok {
			continue
		}
		if found && !aliasKeyBeats(prefix, best.prefix) {
			continue
		}
		best, found = aliasMatch{prefix: prefix, dir: dir, rest: rest}, true
	}
	return best, found
}

// aliasKeyBeats reports whether alias key a takes precedence over key b.
func aliasKeyBeats(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a < b
}

// aliasRest matches one alias key against a specifier and returns what follows
// the key.
//
// A trailing slash marks a key from a wildcard entry ("@/*": ["src/*"]), which
// matches anything it prefixes. A key without one names a single module
// ("@shared": ["src/shared/index"]) and matches only that name or a path under
// it, never a longer name that starts with the same characters.
func aliasRest(prefix, imp string) (string, bool) {
	if strings.HasSuffix(prefix, "/") {
		if strings.HasPrefix(imp, prefix) {
			return imp[len(prefix):], true
		}
		return "", false
	}
	if imp == prefix {
		return "", true
	}
	if strings.HasPrefix(imp, prefix+"/") {
		return imp[len(prefix)+1:], true
	}
	return "", false
}

// isPathAlias returns true if the import matches any configured path alias.
func isPathAlias(tc *tsConfig, imp string) bool {
	_, ok := matchPathAlias(tc, imp)
	return ok
}

// resolvePathAlias expands a path alias import to a workspace-relative path,
// then delegates to the index / label construction.
//
// Resolution order for an alias like "@/utils/helpers" (alias "@/" → "src/"):
//  1. Every key moduleIndexKeys yields for src/utils/helpers.
//  2. Bare index file:     src/utils/helpers/index (no extension)
//  3. Parent directory:    src/utils (handles non-barrel sub-path imports that
//     point to files compiled into the parent package)
//  4. labelForUnindexed for when nothing provides the target.
func resolvePathAlias(
	_ *config.Config,
	ix *resolve.RuleIndex,
	tc *tsConfig,
	imp string,
	from label.Label,
) string {
	m, ok := matchPathAlias(tc, imp)
	if !ok {
		return ""
	}
	targetRel := path.Join(strings.TrimSuffix(m.dir, "/"), m.rest)
	bare := dropTsExtension(targetRel)

	keys := moduleIndexKeys(targetRel, []string{".ts", ".tsx", ".js"})
	// Legacy bare index lookup (no extension).
	keys = append(keys, path.Join(bare, "index"))
	// Sub-path fallback: "@/utils/helpers" might refer to a file compiled into
	// the parent package (//src/utils:utils) rather than a dedicated
	// sub-package. Example: "@/utils/helpers" → "src/utils/helpers" (miss) →
	// try "src/utils" → found //src/utils:utils → return it.
	if parent := path.Dir(bare); parent != "." && parent != bare {
		keys = append(keys, parent)
	}

	for _, key := range keys {
		if lbl, selfImport := lookupInIndex(ix, key, from); lbl != "" {
			return lbl
		} else if selfImport {
			return ""
		}
	}

	return labelForUnindexed(targetRel, from)
}

// ---- npm package resolution ------------------------------------------------

// resolveNpmPackage maps a bare specifier (e.g. "react", "@tanstack/router")
// to a Bazel label. It first checks the npm package mapping (if present),
// then falls back to the default @npm//:target-name convention.
//
// The label format matches what rules_typescript's npm_translate_lock generates:
//   - "vitest"             → "@npm//:vitest" (or the ts_npm_hub hub)
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

	// A specifier carrying a URI scheme is not an npm package. Bundlers
	// synthesise modules behind one ("virtual:routes"), the package is
	// declared ambiently rather than installed, and a Bazel target name
	// cannot contain ':' -- so emitting a label here produces one that
	// fails to parse rather than one that merely does not exist.
	if strings.Contains(imp, ":") {
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

	// A built-in spelled without the prefix is not a package: no hub declares a
	// target, and strict deps resolves it against Node's own builtinModules.
	if isNodeBuiltin(pkgName) {
		return ""
	}

	// Default convention: <hub>//:target-name, the hub being @npm unless
	// directiveNpmHub named another. The target name is derived from the npm
	// package name by dropping the leading "@" for scoped packages and
	// replacing "/" with "_", which matches the _package_name_to_label
	// function in npm_translate_lock.bzl.
	hub := tc.npmHub
	if hub == "" {
		hub = defaultNpmHub
	}
	targetName := npmPackageToLabelName(pkgName)
	return hub + "//:" + targetName
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

// Every bare name the toolchain node reports in `builtinModules`, which is what
// the strict-deps check resolves against: a name missing here becomes an
// `@npm//:<name>` label no hub declares.
var nodeBuiltins = map[string]bool{
	"_http_agent": true, "_http_client": true, "_http_common": true,
	"_http_incoming": true, "_http_outgoing": true, "_http_server": true,
	"_stream_duplex": true, "_stream_passthrough": true, "_stream_readable": true,
	"_stream_transform": true, "_stream_wrap": true, "_stream_writable": true,
	"_tls_common": true, "_tls_wrap": true,
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true,
	"dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true,
	"inspector": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"sys": true, "timers": true, "tls": true, "trace_events": true, "tty": true,
	"url": true, "util": true, "v8": true, "vm": true, "wasi": true,
	"worker_threads": true, "zlib": true,
}

// NodeBuiltins is exported for //tests/strict_deps:checker_test, which compares
// the list against the node the strict-deps check runs.
func NodeBuiltins() []string {
	names := make([]string, 0, len(nodeBuiltins))
	for name := range nodeBuiltins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isNodeBuiltin returns true for Node.js built-in module specifiers such as
// "node:fs", "node:path", "fs", "path", "os", etc. These are never resolvable
// to a Bazel label so they should not produce a warning even when
// warnUnresolved is enabled.
func isNodeBuiltin(imp string) bool {
	if strings.HasPrefix(imp, "node:") {
		return true
	}
	return nodeBuiltins[imp]
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
