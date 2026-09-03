package typescript

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/gazelle/jsonc"
)

// ---- framework detection ---------------------------------------------------

// Framework represents a frontend framework detected from package.json.
type Framework int

const (
	// FrameworkNone means no recognised framework was detected.
	FrameworkNone Framework = iota

	// FrameworkTanStack is set when @tanstack/react-router or @tanstack/start
	// is listed in package.json dependencies.
	FrameworkTanStack

	// FrameworkNextJS is set when "next" is listed in package.json
	// dependencies. Reserved for future use.
	FrameworkNextJS

	// FrameworkRemix is set when @remix-run/dev or @remix-run/react is listed
	// in package.json dependencies.
	FrameworkRemix

	// FrameworkSvelteKit is set when @sveltejs/kit is listed in package.json
	// dependencies.
	FrameworkSvelteKit

	// FrameworkSolidStart is set when @solidjs/start or solid-start is listed
	// in package.json dependencies.
	FrameworkSolidStart
)

// ---- directive keys --------------------------------------------------------

const (
	// directivePackageBoundary controls package-boundary detection mode.
	//   # gazelle:ts_package_boundary           → every-dir (default)
	//   # gazelle:ts_package_boundary every-dir → same as above
	//   # gazelle:ts_package_boundary tsconfig  → one package per tsconfig.json
	//   # gazelle:ts_package_boundary true      → mark this one directory
	directivePackageBoundary = "ts_package_boundary"

	// directiveIgnore suppresses TypeScript rule generation for this directory
	// and its subdirectories.
	//   # gazelle:ts_ignore
	directiveIgnore = "ts_ignore"

	// directiveTargetName overrides the name of the primary ts_compile rule.
	//   # gazelle:ts_target_name my_lib
	directiveTargetName = "ts_target_name"

	// directiveWarnUnresolved controls whether a warning is printed for imports
	// that cannot be resolved to a Bazel label. Accepted values: "true" / "false".
	// Default: false (unresolved imports are silently skipped).
	//   # gazelle:ts_warn_unresolved true
	directiveWarnUnresolved = "ts_warn_unresolved"

	// directiveDeclarations selects the .d.ts emitter on generated ts_compile
	// rules. Accepted values: "tsgo" / "oxc". Default: "tsgo", which is the rule
	// default, so no attribute is emitted. Set to "oxc" once every export in the
	// tree carries an explicit type, to take type-checking off the critical path.
	//   # gazelle:ts_declarations oxc
	directiveDeclarations = "ts_declarations"

	// directivePathAlias adds a TypeScript path alias mapping. The value is
	// "<alias> <dir>" where alias is the path alias prefix (e.g. "@/") and dir
	// is the workspace-relative directory (e.g. "src/"). Multiple directives
	// may appear in a single BUILD file; each one adds to (not replaces) the
	// mapping inherited from the parent directory.
	//   # gazelle:ts_path_alias @/ src/
	//   # gazelle:ts_path_alias @components/ src/components/
	directivePathAlias = "ts_path_alias"

	// directiveRuntimeDep appends a Bazel label to the runtimeDepsTest list,
	// i.e. to every generated ts_test deps list in the directory tree. Use this
	// for packages that are needed at test runtime but are never statically
	// imported (e.g. happy-dom, @vitest/coverage-v8).
	//   # gazelle:ts_runtime_dep @npm//:happy-dom
	directiveRuntimeDep = "ts_runtime_dep"

	// directiveAmbientTypes appends a Bazel label to every generated ts_compile
	// and ts_test deps list in the directory tree. An ambient declaration has no
	// import to infer a dep from, so this is the one dep the resolver cannot
	// derive -- and the reason a migration would otherwise mean editing every
	// target that touches `process` or `Buffer`.
	//   # gazelle:ts_ambient_types @npm//:types_node
	directiveAmbientTypes = "ts_ambient_types"

	// directiveExclude registers an additional file glob pattern to exclude
	// from source targets. The value is a filepath.Match-style pattern matched
	// against the file basename, or -- written with a leading "./" -- against
	// the path relative to the directory whose build file declares it.
	//   # gazelle:ts_exclude *.generated.ts
	//   # gazelle:ts_exclude ./vite.config.ts
	directiveExclude = "ts_exclude"

	// directiveExcludeDir names one directory basename Gazelle does not enter,
	// in addition to the built-in set (.next, .nuxt, .svelte-kit, dist, build).
	// It may appear more than once and appends to the set inherited from the
	// parent directory, so the effective set does not depend on which directory
	// asks for it.
	//
	//	# gazelle:ts_exclude_dir coverage
	//
	// It is declared in an ancestor rather than in the directory it names,
	// because a directory Gazelle should not enter is exactly the kind with no
	// build file to carry a ts_ignore. The value is a basename and not a path:
	// a basename is all the traversal ever compares against, and excluding one
	// named path is what an anchored ts_exclude already does.
	directiveExcludeDir = "ts_exclude_dir"

	// directiveCodegen registers a custom ts_codegen target via a directive.
	// Format: # gazelle:ts_codegen <name> <generator_label> <outs_csv> [srcs:<csv>] [args...]
	// The <outs_csv> field is a comma-separated list of output file names.
	// The optional srcs: field names the generator's inputs; omitted, it reads
	// the directory's own TypeScript sources. Everything after those fields is
	// treated as generator args.
	//
	// Example (single output, args with placeholder substitution):
	//   # gazelle:ts_codegen api_types @npm//:openapi-typescript_bin api-types.ts srcs:openapi.yaml {srcs} -o {out}
	//
	// Example (directory output via out_dir prefix):
	//   # gazelle:ts_codegen prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}
	//
	// When the <outs_csv> value starts with "dir:" the remainder is treated as
	// the out_dir value and the Outs slice is left empty.
	directiveCodegen = "ts_codegen"

	// directiveNpmHub names the repo that npm deps in this tree resolve into.
	//
	//	# gazelle:ts_npm_hub npm_eslint
	//
	// A workspace can have more than one npm hub -- a curated fixture lockfile
	// and a real one, a tool's dependencies kept out of the app's closure -- and
	// which one a package's imports come from is a property of the package, not
	// of the whole repo. Without this the generated label named a hub the
	// package does not use, which is a label that does not exist.
	directiveNpmHub = "ts_npm_hub"

	// directiveNpmMapping names a JSON file, workspace-root-relative, mapping
	// npm package names to Bazel label strings:
	//
	//	# gazelle:ts_npm_mapping npm/package_mapping.json
	//
	// It overlays the lockfile inventory rather than replacing it: a name the
	// file gives a label keeps that label, and every name it leaves out keeps
	// the lockfile's answer. Root-relative because its values are workspace
	// labels, so the file is a workspace-level artifact wherever it is named.
	// Repeatable, and inherited: a subtree can overlay again on top of what an
	// ancestor mapped.
	directiveNpmMapping = "ts_npm_mapping"

	// directiveAssetDeclarationType hands Gazelle one entry of the
	// declaration_type dict on every asset_library in this tree, generated or
	// already written, so an svgr-style project declares the type once rather
	// than on each of the one-target-per-asset-file rules.
	//
	//	# gazelle:ts_asset_declaration_type .svg import("react").FC<import("react").SVGProps<SVGSVGElement>>
	//
	// Only the first space separates: everything after the extension is the
	// type expression verbatim, so `{ default: string }` needs no quoting.
	// The extension alone declares that this tree resolves it to nothing in
	// particular, and Gazelle removes the entry.
	directiveAssetDeclarationType = "ts_asset_declaration_type"

	// directiveJSSrcs admits JavaScript sources into the srcs of the targets
	// generated in this tree. The value is the set of extensions to admit:
	//
	//	# gazelle:ts_js_srcs .mjs .cjs
	//
	// ts_compile has always accepted .js/.mjs/.cjs in srcs, so this is a policy
	// question and not a capability one: admitting them everywhere would put
	// eslint.config.mjs and postcss.config.mjs into the type program of every
	// repo that never asked for it. Named with nothing after it the directive
	// admits none of them, which is how a subtree opts back out.
	directiveJSSrcs = "ts_js_srcs"
)

// packageBoundaryMode values.
const (
	// boundaryEveryDir is the default: every directory with .ts files gets a
	// ts_compile target.
	boundaryEveryDir = "every-dir"

	// boundaryTsConfig makes a directory a package when it holds a
	// tsconfig.json, so one Bazel target covers one TypeScript project. It is
	// the only mode that can express a project whose ambient declaration and
	// its sources sit in different directories, or whose directories import
	// each other -- both legal in a single tsc program, and a cycle once every
	// directory is a target of its own.
	boundaryTsConfig = "tsconfig"
)

// boundaryFromDirective reads a ts_package_boundary value: the mode it names,
// or marksDir for "true", which marks the one directory and leaves the mode
// alone. An unrecognised value is an error rather than the inherited mode,
// because a directive that quietly does nothing leaves a tree compiling to
// something other than what its author wrote.
func boundaryFromDirective(value string) (mode string, marksDir bool, err error) {
	switch strings.TrimSpace(value) {
	case "", boundaryEveryDir:
		return boundaryEveryDir, false, nil
	case boundaryTsConfig:
		return boundaryTsConfig, false, nil
	case "true":
		return "", true, nil
	case "index-only":
		return "", false, fmt.Errorf("ts_package_boundary index-only was removed; the modes are %q and %q",
			boundaryEveryDir, boundaryTsConfig)
	default:
		return "", false, fmt.Errorf("unknown ts_package_boundary value %q; want %q, %q, or \"true\"",
			value, boundaryEveryDir, boundaryTsConfig)
	}
}

// ---- per-directory configuration -------------------------------------------

// tsConfig holds the TypeScript-specific Gazelle configuration for a single
// directory. An instance is stored in config.Config.Exts keyed by languageName
// and is inherited (shallow-copied) through the directory hierarchy.
type tsConfig struct {
	// detectedFramework is the framework detected from the workspace-root
	// package.json. Populated once at the repo root and inherited by all
	// descendant directories via the clone mechanism. The zero value
	// (FrameworkNone) means no framework was detected.
	detectedFramework Framework

	// svelteKitAssets is the directory kit.files.assets names in
	// svelte.config.js -- a documented relocatable option, so the default
	// "static" cannot be assumed. Read once at the root and inherited.
	svelteKitAssets string

	// packageBoundaryMode controls how package boundaries are detected:
	// boundaryEveryDir (the default) or boundaryTsConfig.
	packageBoundaryMode string

	// packageBoundary indicates that this specific directory is an explicit
	// package boundary. It is what makes a directory a package in tsconfig
	// mode when the tsconfig.json covering it sits somewhere else.
	packageBoundary bool

	// npmHub is the repo label prefix that a bare specifier resolves into,
	// e.g. "@npm". Set by directiveNpmHub and inherited by child directories.
	npmHub string

	// ignore suppresses ts_compile / ts_test generation in this directory.
	ignore bool

	// targetName overrides the default target name (which is the directory
	// basename). Empty means use the default.
	targetName string

	// pathAliases maps a TypeScript path alias prefix (e.g. "@/") to a
	// workspace-relative directory path (e.g. "src/"). Can be populated from
	// tsconfig.json or # gazelle:ts_path_alias directives. Directives take
	// priority over the file-based source.
	pathAliases map[string]string

	// aliasesFromDirectives records that pathAliases was declared rather than
	// read back out of a tsconfig this ruleset generated. Inherited downward.
	aliasesFromDirectives bool

	// importsAliases are the pathAliases entries the nearest package.json
	// "imports" map contributed, so a nearer one can replace them: Node
	// answers a "#" specifier from the nearest enclosing package.json.
	importsAliases map[string]string

	// importsNpm maps a "#" specifier from that same map onto the package
	// specifier its target names, for the entries whose target is another
	// package rather than a path inside this one.
	importsNpm map[string]string

	// npmPackages holds the set of npm package names known to the workspace.
	// Keys are npm package names (e.g. "react"). A value is the Bazel label to
	// use as a dep (e.g. "@npm//react"); "" means the entry asserts only that
	// the hub declares this name, so the label comes from the npmHub convention
	// and a ts_npm_hub directive still gets to choose the repository.
	// pnpm-lock.yaml supplies "" for everything it declares, while a
	// # gazelle:ts_npm_mapping file supplies real labels and overrides the
	// lockfile per key.
	//
	// nil is a weaker claim than an empty map: no inventory could be read at
	// all, rather than a lockfile that declares nothing. See loadNpmInventory.
	//
	// Populated once per Gazelle run and then shared across all directories via
	// pointer-equality (never mutated after load).
	npmPackages map[string]string

	// npmLockNames is every package name the lockfile mentions, read from the
	// same file at the same time. Where npmPackages says which names the hub
	// declares a target for and under-claims on purpose, this one says which
	// names the workspace has ever heard of and over-claims on purpose: it is
	// read only to refuse a hub label, so its errors have to fall on the side
	// of not refusing. nil means no lockfile answered and nothing is refused.
	npmLockNames map[string]bool

	// workspaceMembers is the set of workspace-relative directories the
	// lockfile lists as pnpm importers -- the workspace's own packages. Read
	// alongside npmPackages and shared the same way. nil means no lockfile
	// answered, under which a package name is treated as installed.
	workspaceMembers map[string]bool

	// npmInventoryLoaded records that the lockfile read was attempted, so the
	// walk does not re-read pnpm-lock.yaml once per directory. Copied by
	// clone(), which is what leaves the first Configure call the only one that
	// touches the file.
	npmInventoryLoaded bool

	// warnUnresolved controls whether a warning is emitted for imports that
	// cannot be resolved to any Bazel label. When false (the default) such
	// imports are silently skipped. Enable via:
	//   # gazelle:ts_warn_unresolved true
	warnUnresolved bool

	// excludePatterns holds the file glob patterns to exclude from source
	// targets, from # gazelle:ts_exclude directives. A directive appends to the
	// inherited list, and each entry remembers the directory that declared it
	// -- what an anchored pattern resolves against and the only build file
	// where editing the directive changes anything.
	excludePatterns []excludeRule

	// excludeDirs holds directory basenames that should be excluded from
	// Gazelle traversal, from # gazelle:ts_exclude_dir directives. Directives
	// append to the inherited list. The built-in set (.next, .nuxt,
	// .svelte-kit, dist, build) is always excluded regardless of this setting.
	excludeDirs []string

	// linterConfig is the workspace-relative path to the nearest linter
	// config file found in the current directory or any ancestor directory.
	// Empty means no linter config was detected.
	// Supported files: oxlint.json, .oxlintrc.json, eslint.config.mjs,
	// eslint.config.js, eslint.config.cjs, .eslintrc.js, .eslintrc.json,
	// .eslintrc.yaml, .eslintrc.yml, .eslintrc.cjs, .eslintrc
	linterConfig string

	// linterType is "oxlint" or "eslint", derived from linterConfig's filename.
	// Empty when no linter config is detected.
	linterType string

	// tsConfigFile is the workspace-relative path of the nearest hand-written
	// tsconfig.json in this directory or an ancestor -- the compilerOptions
	// baseline a target generated here should compile under. Empty leaves those
	// targets on the ruleset's own baseline.
	tsConfigFile string

	// runtimeDepsTest is the list of additional Bazel label strings that
	// should be appended to every generated ts_test deps list. Populated from
	// # gazelle:ts_runtime_dep directives, which append to the list.
	// Use this for packages that are needed at test runtime but are never
	// statically imported (e.g. "happy-dom", "@vitest/coverage-v8").
	runtimeDepsTest []string

	// ambientTypes is the list of Bazel label strings appended to every
	// generated ts_compile and ts_test deps list in this tree, for @types
	// packages whose declarations are ambient and so have no import.
	ambientTypes []string

	// tsconfigAmbientTypes is the same thing read out of the nearest
	// tsconfig.json rather than declared by a directive. Kept apart because the
	// two combine differently down the tree: a directive appends to what it
	// inherits, while a tsconfig replaces it, the way tsc gives a file exactly
	// one project.
	tsconfigAmbientTypes []string

	// tsconfigTypes is that same key unread, tsconfigTypesDir the directory
	// its entries are written relative to, tsconfigTypeFiles the ones naming
	// a declaration file that directory holds, and tsconfigTypeGenerators the
	// ones naming a file a ts_worker_types target there writes, keyed by file
	// name with the target's name as the value. Empty unless every file-shaped
	// entry is one or the other.
	tsconfigTypes          []string
	tsconfigTypesDir       string
	tsconfigTypeFiles      []string
	tsconfigTypeGenerators map[string]string

	// declarations is the .d.ts emitter for generated ts_compile rules:
	// "tsgo" (default, no attribute emitted) or "oxc". Set via
	// # gazelle:ts_declarations.
	declarations string

	// assetDeclarationType maps an asset extension (leading dot) to the
	// TypeScript type expression asset_library.declaration_type carries for it
	// in this tree. A key present with an empty value is the extension a
	// directive named and left blank: still Gazelle's, declaring nothing.
	// Set via # gazelle:ts_asset_declaration_type.
	assetDeclarationType map[string]string

	// jsSrcExts are the extensions a ts_js_srcs directive admits into the srcs
	// of the targets generated in this tree, lowercased and dot-led. Empty --
	// the default -- leaves srcs at .ts and .tsx.
	jsSrcExts []string

	// customCodegens holds ts_codegen patterns parsed from
	// # gazelle:ts_codegen directives. Each directive contributes one entry.
	// Format: # gazelle:ts_codegen <name> <generator_label> <outs_csv> [srcs:<csv>] [args...]
	// Example: # gazelle:ts_codegen api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}
	// These patterns are appended to whatever detectCodegen returns, each in
	// the one directory it was declared in.
	customCodegens []CodegenPattern
}

// getConfig retrieves the tsConfig from a config.Config. Returns a default
// tsConfig if none has been set yet (i.e. Configure was not called).
func getConfig(c *config.Config) *tsConfig {
	if v, ok := c.Exts[languageName]; ok {
		return v.(*tsConfig)
	}
	return &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		declarations:        "tsgo",
		npmHub:              defaultNpmHub,
	}
}

// clone returns a copy of the config, suitable for child directories that
// inherit from their parent.
func (tc *tsConfig) clone() *tsConfig {
	cp := *tc
	// npmPackages, npmLockNames, workspaceMembers, importsAliases and
	// importsNpm are read-only after construction; sharing via pointer is safe.
	//
	// pathAliases can be extended or replaced by per-directory directives, so
	// we must deep-copy it to ensure that a child's mutation (merge or replace)
	// does not corrupt the parent's map.
	if tc.pathAliases != nil {
		cp.pathAliases = make(map[string]string, len(tc.pathAliases))
		for k, v := range tc.pathAliases {
			cp.pathAliases[k] = v
		}
	}
	// Slices that can be extended by per-directory directives (excludePatterns,
	// runtimeDepsTest) must be copied so that a child's append does not mutate
	// the parent's slice backing array.
	if len(tc.excludePatterns) > 0 {
		cp.excludePatterns = make([]excludeRule, len(tc.excludePatterns))
		copy(cp.excludePatterns, tc.excludePatterns)
	}
	if len(tc.ambientTypes) > 0 {
		cp.ambientTypes = make([]string, len(tc.ambientTypes))
		copy(cp.ambientTypes, tc.ambientTypes)
	}
	if len(tc.tsconfigAmbientTypes) > 0 {
		cp.tsconfigAmbientTypes = make([]string, len(tc.tsconfigAmbientTypes))
		copy(cp.tsconfigAmbientTypes, tc.tsconfigAmbientTypes)
	}
	if len(tc.tsconfigTypes) > 0 {
		cp.tsconfigTypes = make([]string, len(tc.tsconfigTypes))
		copy(cp.tsconfigTypes, tc.tsconfigTypes)
	}
	if len(tc.tsconfigTypeFiles) > 0 {
		cp.tsconfigTypeFiles = make([]string, len(tc.tsconfigTypeFiles))
		copy(cp.tsconfigTypeFiles, tc.tsconfigTypeFiles)
	}
	if len(tc.tsconfigTypeGenerators) > 0 {
		cp.tsconfigTypeGenerators = make(map[string]string, len(tc.tsconfigTypeGenerators))
		for name, target := range tc.tsconfigTypeGenerators {
			cp.tsconfigTypeGenerators[name] = target
		}
	}
	if len(tc.runtimeDepsTest) > 0 {
		cp.runtimeDepsTest = make([]string, len(tc.runtimeDepsTest))
		copy(cp.runtimeDepsTest, tc.runtimeDepsTest)
	}
	if len(tc.excludeDirs) > 0 {
		cp.excludeDirs = make([]string, len(tc.excludeDirs))
		copy(cp.excludeDirs, tc.excludeDirs)
	}
	// A child directive adds, overrides or clears one extension, so the map has
	// to be the child's own before configureTsConfig writes into it.
	if tc.assetDeclarationType != nil {
		cp.assetDeclarationType = make(map[string]string, len(tc.assetDeclarationType))
		for k, v := range tc.assetDeclarationType {
			cp.assetDeclarationType[k] = v
		}
	}
	// customCodegens is inherited but not mutated after construction (each
	// directory's directive appends a new entry to the child copy).
	if len(tc.customCodegens) > 0 {
		cp.customCodegens = make([]CodegenPattern, len(tc.customCodegens))
		copy(cp.customCodegens, tc.customCodegens)
	}
	return &cp
}

// ---- package.json framework detection -------------------------------------

// packageJSON is a minimal representation of package.json used only for
// framework detection. Only the fields we need are decoded.
type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// detectFramework reads the workspace-root package.json (if present) and
// returns the framework it implies, or FrameworkNone.
//
// Detection rules (checked in order of priority):
//   - @tanstack/start or @tanstack/react-router → FrameworkTanStack
//   - @remix-run/dev or @remix-run/react        → FrameworkRemix
//   - @sveltejs/kit                             → FrameworkSvelteKit
//   - @solidjs/start or solid-start             → FrameworkSolidStart
//   - next                                      → FrameworkNextJS
func detectFramework(repoRoot string) Framework {
	data, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		// No package.json at root — not a framework project.
		return FrameworkNone
	}
	var pj packageJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		log.Printf("typescript: failed to parse workspace root package.json: %v", err)
		return FrameworkNone
	}

	// Merge deps and devDeps into one map for a single-pass check.
	allDeps := make(map[string]string, len(pj.Dependencies)+len(pj.DevDependencies))
	for k, v := range pj.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pj.DevDependencies {
		allDeps[k] = v
	}

	// TanStack takes priority over Next.js in case both appear.
	if _, ok := allDeps["@tanstack/start"]; ok {
		return FrameworkTanStack
	}
	if _, ok := allDeps["@tanstack/react-router"]; ok {
		return FrameworkTanStack
	}
	// Remix detection.
	if _, ok := allDeps["@remix-run/dev"]; ok {
		return FrameworkRemix
	}
	if _, ok := allDeps["@remix-run/react"]; ok {
		return FrameworkRemix
	}
	// SvelteKit detection.
	if _, ok := allDeps["@sveltejs/kit"]; ok {
		return FrameworkSvelteKit
	}
	// SolidStart detection.
	if _, ok := allDeps["@solidjs/start"]; ok {
		return FrameworkSolidStart
	}
	if _, ok := allDeps["solid-start"]; ok {
		return FrameworkSolidStart
	}
	if _, ok := allDeps["next"]; ok {
		return FrameworkNextJS
	}
	return FrameworkNone
}

// ---- linter config detection -----------------------------------------------

// oxlintConfigNames is the ordered list of filenames recognized as oxlint
// configuration files.
var oxlintConfigNames = []string{
	"oxlint.json",
	".oxlintrc.json",
	".oxlintrc",
}

// eslintConfigNames is the ordered list of filenames recognized as ESLint
// configuration files (flat config and legacy formats).
var eslintConfigNames = []string{
	"eslint.config.mjs",
	"eslint.config.js",
	"eslint.config.cjs",
	".eslintrc.js",
	".eslintrc.cjs",
	".eslintrc.yaml",
	".eslintrc.yml",
	".eslintrc.json",
	".eslintrc",
}

// detectLinterConfig scans dir and then each ancestor up to (but not
// including) repoRoot looking for a known linter config file.
// Returns (workspaceRelPath, linterType) or ("", "") if not found.
// oxlint is checked before eslint because oxlint.json is a superset of
// neither but its users are more likely to have oxlint installed.
func detectLinterConfig(repoRoot, dir string) (string, string) {
	for {
		// Check oxlint first (faster, Rust-based).
		for _, name := range oxlintConfigNames {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				rel, _ := filepath.Rel(repoRoot, candidate)
				return rel, "oxlint"
			}
		}
		// Check eslint.
		for _, name := range eslintConfigNames {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				rel, _ := filepath.Rel(repoRoot, candidate)
				return rel, "eslint"
			}
		}
		// Stop at the repo root.
		if dir == repoRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", ""
}

// detectLinterConfigInDir checks only the single directory dir (no ancestor
// walk) for a known linter config file. Returns (workspaceRelPath, linterType)
// or ("", "") if not found. repoRoot is used to compute the relative path.
func detectLinterConfigInDir(dir, repoRoot string) (string, string) {
	for _, name := range oxlintConfigNames {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			rel, _ := filepath.Rel(repoRoot, candidate)
			return rel, "oxlint"
		}
	}
	for _, name := range eslintConfigNames {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			rel, _ := filepath.Rel(repoRoot, candidate)
			return rel, "eslint"
		}
	}
	return "", ""
}

// linterBinaryLabel is the hub's bin alias for the linter package -- linterType
// is the package name -- in the hub the tree resolves its bare imports into.
func linterBinaryLabel(tc *tsConfig) string {
	return npmHubLabel(tc, tc.linterType) + "_bin"
}

var linterNotInLockfileReported sync.Map

// reportLinterNotInLockfile says why no ts_lint follows this config, once per
// config file: the config is inherited by every directory below it.
func reportLinterNotInLockfile(repoRoot string, tc *tsConfig) {
	if _, done := linterNotInLockfileReported.LoadOrStore(filepath.Join(repoRoot, tc.linterConfig), true); done {
		return
	}
	log.Printf("typescript: %s: no ts_lint is generated for the directories it covers -- %s is "+
		"not in %s, so %s is a target the hub does not declare, and Bazel answers a rule "+
		"naming it with `no such target`, which fails analysis for the whole package. "+
		"Add %s to the workspace's dependencies, or delete the config.",
		tc.linterConfig, tc.linterType, pnpmLockfileName, linterBinaryLabel(tc), tc.linterType)
}

// linterConfigLabel converts a workspace-relative linter config path to a
// Bazel label string. Returns empty string when configPath is empty.
// Paths in the repo root become "//:filename"; paths in subdirectories become
// "//sub/dir:filename".
func linterConfigLabel(configPath string) string {
	if configPath == "" {
		return ""
	}
	// Normalize to forward slashes for Bazel label construction.
	configPath = strings.ReplaceAll(configPath, string(filepath.Separator), "/")
	dir := path.Dir(configPath)
	base := path.Base(configPath)
	if dir == "." || dir == "" {
		return "//:" + base
	}
	return "//" + dir + ":" + base
}

// ---- the compilerOptions baseline ------------------------------------------

// tsConfigTargetName is the ts_config target Gazelle writes beside a package's
// own tsconfig.json, and the one a target under it names.
const tsConfigTargetName = "tsconfig"

// generatedTsConfigMarker is the _comment ts_refresh_tsconfig stamps on every
// tsconfig.json it writes (ts/private/tsconfig_aspect.bzl, _HEADER).
const generatedTsConfigMarker = "bazel run //:refresh_tsconfig"

// isGeneratedTsConfig reports whether path is a tsconfig.json this ruleset
// writes. Naming one as a baseline is a cycle spelled as a baseline: it is built
// out of the very targets that would name it, and the `extends` chain it carries
// reaches files no target declares.
func isGeneratedTsConfig(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		Comment string `json:"_comment"`
	}
	if err := jsonc.Unmarshal(data, &doc); err != nil {
		return false
	}
	return strings.Contains(doc.Comment, generatedTsConfigMarker)
}

// handWrittenTsConfigIn returns the workspace-relative path of dir's own
// tsconfig.json, or "" when it has none or the one it has is generated.
func handWrittenTsConfigIn(dir, repoRoot string) string {
	candidate := filepath.Join(dir, "tsconfig.json")
	if st, err := os.Stat(candidate); err != nil || st.IsDir() {
		return ""
	}
	if isGeneratedTsConfig(candidate) {
		return ""
	}
	rel, err := filepath.Rel(repoRoot, candidate)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// nearestHandWrittenTsConfig walks dir and then each ancestor up to and
// including repoRoot.
func nearestHandWrittenTsConfig(repoRoot, dir string) string {
	for {
		if found := handWrittenTsConfigIn(dir, repoRoot); found != "" {
			return found
		}
		if dir == repoRoot {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ---- tsconfig.json reading -------------------------------------------------

type tsConfigJSON struct {
	Extends         tsConfigExtends `json:"extends"`
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
		// A pointer because "types": [] and no "types" key at all mean
		// opposite things to tsc: none, versus every @types package in scope.
		Types *[]string `json:"types"`
	} `json:"compilerOptions"`
}

// tsConfigExtends is the list of configs a tsconfig inherits from, written as
// one specifier or, since TypeScript 5.0, an array of them.
type tsConfigExtends []string

func (e *tsConfigExtends) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*e = tsConfigExtends{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*e = many
	return nil
}

// resolvedTsConfig is an extends chain flattened to the compilerOptions that
// win, each paired with the directory of the config that wrote it: a relative
// value resolves against its own file, not against the leaf.
type resolvedTsConfig struct {
	baseURL    string
	baseURLDir string
	paths      map[string][]string
	pathsDir   string
}

// resolveTsConfigChain reads a tsconfig and, depth first, the configs it
// extends, and returns what a leaf-wins merge leaves standing. tsc replaces an
// inherited compilerOptions key wholesale instead of merging it key by key, so
// paths always arrives from exactly one file in the chain.
func resolveTsConfigChain(tsConfigPath string, ancestors map[string]bool) *resolvedTsConfig {
	tsConfigPath = filepath.Clean(tsConfigPath)
	// Only an ancestor repeat is a cycle. A config reached twice down two
	// branches is read twice, because merge order decides which one wins.
	if ancestors[tsConfigPath] {
		return nil
	}
	ancestors[tsConfigPath] = true
	defer delete(ancestors, tsConfigPath)

	data, err := os.ReadFile(tsConfigPath)
	if err != nil {
		return nil
	}
	var tsc tsConfigJSON
	if err := jsonc.Unmarshal(data, &tsc); err != nil {
		log.Printf("typescript: failed to parse %s: %v", tsConfigPath, err)
		return nil
	}

	dir := filepath.Dir(tsConfigPath)
	resolved := &resolvedTsConfig{}
	for _, spec := range tsc.Extends {
		basePath, ok := resolveExtendsSpecifier(dir, spec)
		if !ok {
			continue
		}
		if base := resolveTsConfigChain(basePath, ancestors); base != nil {
			resolved.override(base)
		}
	}
	resolved.override(&resolvedTsConfig{
		baseURL:    tsc.CompilerOptions.BaseURL,
		baseURLDir: dir,
		paths:      tsc.CompilerOptions.Paths,
		pathsDir:   dir,
	})
	return resolved
}

func (r *resolvedTsConfig) override(other *resolvedTsConfig) {
	if other.baseURL != "" {
		r.baseURL, r.baseURLDir = other.baseURL, other.baseURLDir
	}
	if other.paths != nil {
		r.paths, r.pathsDir = other.paths, other.pathsDir
	}
}

// resolveExtendsSpecifier turns an extends value into a path on disk. A bare or
// scoped specifier resolves through node_modules, which a Bazel checkout does
// not have, so it is reported and skipped.
func resolveExtendsSpecifier(dir, spec string) (string, bool) {
	if spec == "" {
		return "", false
	}
	relative := strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")
	if !relative && !filepath.IsAbs(spec) {
		warnNodeModulesExtends(dir, spec)
		return "", false
	}
	if !strings.HasSuffix(spec, ".json") {
		spec += ".json"
	}
	if !relative {
		return spec, true
	}
	return filepath.Join(dir, filepath.FromSlash(spec)), true
}

var nodeModulesExtendsWarned sync.Map

func warnNodeModulesExtends(dir, spec string) {
	if _, warned := nodeModulesExtendsWarned.LoadOrStore(spec, true); warned {
		return
	}
	log.Printf("typescript: the tsconfig in %s extends %q, which resolves through node_modules; "+
		"Gazelle reads only configs on disk and skips it. Any paths or baseUrl that config "+
		"contributes are missing from the generated targets: inline them, or extend a "+
		"checked-in config instead.", dir, spec)
}

// loadTsConfigPaths reads compilerOptions.paths and compilerOptions.baseUrl
// from a tsconfig.json file and the chain of configs it extends. The baseUrl
// (if present) is used to resolve the target directories in the paths entries.
// Returns nil when the file does not exist or the chain has no paths.
//
// The paths format in tsconfig is:
//
//	"@/*": ["src/*"]
//	"@components/*": ["src/components/*"]
//
// We convert each path pattern to the simpler prefix→dir form used by tsConfig.pathAliases:
//   - Strip trailing "/*" from both the alias key and the chosen target value.
//   - Reduce the fallback array to one target with pickAliasTarget.
//   - Prepend baseUrl to the target directory when baseUrl is non-empty.
//   - Prepend pkgRel, the tsconfig's own directory relative to the repo root.
//
// That last step is what makes the result a Bazel path. A tsconfig's `paths`
// are written relative to the tsconfig; the aliases feed label construction,
// which is relative to the repo root. Those coincide only when the tsconfig is
// at the repo root -- not the case for a workspace member such as `web/`,
// where "./shared/*" means web/shared, and a label of //shared names nothing.
//
// Examples (baseUrl = ""):
//
//	"@/*": ["src/*"]          → "@/" → "src/"
//	"@components/*": ["src/components/*"] → "@components/" → "src/components/"
//	"@lib": ["src/lib"]       → "@lib" → "src/lib"
//
// Examples (baseUrl = "src"):
//
//	"@/*": ["./*"]            → "@/" → "src/"
//	"utils": ["utils/index"]  → "utils" → "src/utils/index"
func loadTsConfigPaths(tsConfigPath, pkgRel string) map[string]string {
	resolved := resolveTsConfigChain(tsConfigPath, map[string]bool{})
	if resolved == nil || len(resolved.paths) == 0 {
		return nil
	}

	baseURL := strings.TrimSuffix(resolved.baseURL, "/")

	// Targets hang off the directory of the config that wrote the value they
	// are relative to, which stops being the leaf as soon as extends is used.
	originDir := resolved.pathsDir
	if baseURL != "" {
		originDir = resolved.baseURLDir
	}
	originRel := repoRelDir(pkgRel, filepath.Dir(tsConfigPath), originDir)
	if strings.HasPrefix(originRel, "../") {
		log.Printf("typescript: %s inherits paths from %s, outside the repository; "+
			"no label can name that directory, so no path_alias is emitted.", tsConfigPath, originDir)
		return nil
	}

	baseDir := originDir
	if baseURL != "" && !filepath.IsAbs(baseURL) {
		baseDir = filepath.Join(baseDir, filepath.FromSlash(baseURL))
	}

	// Two patterns can normalise to the same alias key, so iteration order
	// decides which entry survives, and which order the log lines come out in.
	patterns := make([]string, 0, len(resolved.paths))
	for aliasPattern := range resolved.paths {
		patterns = append(patterns, aliasPattern)
	}
	sort.Strings(patterns)

	aliases := make(map[string]string, len(resolved.paths))
	for _, aliasPattern := range patterns {
		targets := resolved.paths[aliasPattern]
		if len(targets) == 0 {
			continue
		}
		target := pickAliasTarget(baseDir, aliasPattern, targets)
		if target == "" {
			continue
		}

		// Strip trailing "/*" wildcard from both sides.
		aliasKey := strings.TrimSuffix(aliasPattern, "/*")
		targetDir := strings.TrimSuffix(target, "/*")

		// Strip leading "./" from the target.
		targetDir = strings.TrimPrefix(targetDir, "./")

		// Prepend baseUrl when set and target is not absolute.
		if baseURL != "" && !strings.HasPrefix(targetDir, "/") {
			if targetDir == "." || targetDir == "" {
				targetDir = baseURL
			} else {
				targetDir = baseURL + "/" + targetDir
			}
		}

		// An identity mapping is not an alias. ts_refresh_tsconfig emits two
		// paths entries per first-party package: the wildcard form maps the
		// package path to itself, and the bare form maps it to its own entry
		// point. Echoing either into every generated target's path_aliases
		// churns every BUILD file and tells Gazelle nothing it cannot read
		// off the package path.
		normKey := strings.TrimSuffix(aliasKey, "/")
		normDir := strings.TrimSuffix(targetDir, "/")
		if normKey == normDir || normKey == strings.TrimSuffix(normDir, "/index") {
			continue
		}

		// Ensure the alias key ends with "/" only when it was a wildcard pattern.
		if strings.HasSuffix(aliasPattern, "/*") && !strings.HasSuffix(aliasKey, "/") {
			aliasKey = aliasKey + "/"
		}
		// Ensure the target dir ends with "/" when the alias has a wildcard.
		if strings.HasSuffix(aliasPattern, "/*") && !strings.HasSuffix(targetDir, "/") {
			targetDir = targetDir + "/"
		}

		// From tsconfig-relative to repo-relative, which is what a label needs.
		if originRel != "" && originRel != "." && !strings.HasPrefix(targetDir, "/") {
			targetDir = path.Join(originRel, targetDir) + trailingSlash(targetDir)
		}

		if aliasKey != "" {
			aliases[aliasKey] = targetDir
		}
	}
	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

// packageImports is one package.json "imports" map, split by what each target
// names: a path inside the package, or another package.
type packageImports struct {
	// "#shared/*": "./shared/*" → "#shared/" → "<pkgRel>/shared/"
	aliases map[string]string
	// "#dep": "lodash" → "#dep" → "lodash"
	npm map[string]string
}

// loadPackageImports reads the "imports" field of the package.json in dir.
//
// A specifier starting with "#" is resolvable only through this map -- Node
// calls it a package-private import, and no lookup outside the package can
// answer one. Left unread, "#shared/x" is a bare specifier, and a bare
// specifier is an npm package: the resolver emits @npm//:#shared, a label whose
// target no hub declares.
func loadPackageImports(dir, pkgRel string) *packageImports {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pj struct {
		Imports map[string]json.RawMessage `json:"imports"`
	}
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil
	}

	loaded := &packageImports{
		aliases: make(map[string]string, len(pj.Imports)),
		npm:     make(map[string]string, len(pj.Imports)),
	}
	for specifier, raw := range pj.Imports {
		if !strings.HasPrefix(specifier, "#") {
			continue
		}
		target := pickImportsTarget(raw)
		if target == "" {
			continue
		}
		key := strings.TrimSuffix(specifier, "/*")
		stem := strings.TrimSuffix(target, "/*")
		// Node allows "*" anywhere in the pattern, but an alias key matches by
		// prefix, so only a trailing one survives the translation.
		if strings.Contains(key, "*") || strings.Contains(stem, "*") {
			continue
		}
		if strings.HasSuffix(specifier, "/*") {
			key += "/"
			stem += "/"
		}
		switch {
		case strings.HasPrefix(target, "./"):
			relDir := strings.TrimPrefix(stem, "./")
			if pkgRel != "" {
				relDir = path.Join(pkgRel, relDir) + trailingSlash(relDir)
			}
			loaded.aliases[key] = relDir
		case isNpmSpecifier(target):
			loaded.npm[key] = stem
		}
	}
	if len(loaded.aliases) == 0 && len(loaded.npm) == 0 {
		return nil
	}
	return loaded
}

// isNpmSpecifier reports whether an "imports" target names another package
// rather than a path inside this one -- {"#dep": "lodash"}, the shape a
// conditional polyfill swap takes.
func isNpmSpecifier(target string) bool {
	switch {
	case target == "":
		return false
	case strings.HasPrefix(target, "."), strings.HasPrefix(target, "/"), strings.HasPrefix(target, "#"):
		return false
	case strings.HasPrefix(target, "@"):
		return strings.Contains(target, "/")
	}
	return true
}

// importsConditions are the conditional-export keys Gazelle reads, in the order
// it prefers them. A build reads a module for its types and a bundler takes the
// ESM branch, and both are one file per entry in every map seen in the wild.
var importsConditions = []string{"types", "import", "module", "default", "node", "require"}

// pickImportsTarget reduces one "imports" value -- a path, a conditions object,
// or an array of either -- to the single path the alias maps to.
func pickImportsTarget(raw json.RawMessage) string {
	var target string
	if err := json.Unmarshal(raw, &target); err == nil {
		return target
	}

	var conditions map[string]json.RawMessage
	if err := json.Unmarshal(raw, &conditions); err == nil {
		for _, name := range importsConditions {
			if nested, ok := conditions[name]; ok {
				if picked := pickImportsTarget(nested); picked != "" {
					return picked
				}
			}
		}
		return ""
	}

	var alternatives []json.RawMessage
	if err := json.Unmarshal(raw, &alternatives); err == nil {
		for _, alternative := range alternatives {
			if picked := pickImportsTarget(alternative); picked != "" {
				return picked
			}
		}
	}
	return ""
}

func repoRelDir(pkgRel, leafDir, dir string) string {
	if dir == leafDir {
		return pkgRel
	}
	rel, err := filepath.Rel(leafDir, dir)
	if err != nil {
		return pkgRel
	}
	return path.Join(pkgRel, filepath.ToSlash(rel))
}

// pickAliasTarget reduces a compilerOptions.paths fallback array to the single
// directory Gazelle resolves the alias against, or "" to drop the alias. It
// prefers the first entry that exists on disk, and falls back to the first
// usable entry when none do -- an alias may legitimately point at a directory
// that only a codegen action produces.
func pickAliasTarget(baseDir, aliasPattern string, targets []string) string {
	usable := make([]string, 0, len(targets))
	for _, target := range targets {
		if aliasTargetIsUsable(target) {
			usable = append(usable, target)
		}
	}
	if len(usable) == 0 {
		// A tool-managed dot-directory is meant to be dropped, and every npm
		// declaration ts_refresh_tsconfig writes takes that path. An alias left
		// with only output-tree entries is the one worth a word: dropping it
		// silently replaces ts_compile's analysis error with a missing dep edge.
		if !anyToolManaged(targets) {
			log.Printf("typescript: paths entry %q has no target Gazelle can use (%v); no path_alias emitted. "+
				"An alias under bazel-out/bazel-bin points into the output tree: set module_name on the "+
				"target that produces those declarations and import it by that name instead.",
				aliasPattern, targets)
		}
		return ""
	}

	onDisk := make([]string, 0, len(usable))
	for _, target := range usable {
		if aliasTargetExists(baseDir, target) {
			onDisk = append(onDisk, target)
		}
	}
	switch len(onDisk) {
	case 0:
		return usable[0]
	case 1:
		return onDisk[0]
	default:
		log.Printf("typescript: paths entry %q resolves on disk to %d directories; using %q and ignoring %v. "+
			"Gazelle emits one directory per alias; if imports must resolve through more than one, "+
			"split the alias or list the extra files in path_alias_srcs.",
			aliasPattern, len(onDisk), onDisk[0], onDisk[1:])
		return onDisk[0]
	}
}

// aliasTargetIsUsable rejects the two shapes that can never become a legal
// path_aliases value.
func aliasTargetIsUsable(target string) bool {
	head, _, _ := strings.Cut(aliasTargetPath(target), "/")

	// bazel-out, bazel-bin, bazel-testlogs and bazel-<workspace> are the
	// convenience symlinks; ts_compile fails analysis on an alias under them.
	if strings.HasPrefix(head, "bazel-") {
		return false
	}

	// A named dot-directory is tool-managed, never a Bazel package:
	// ts_refresh_tsconfig installs npm declarations under npm_dir
	// (.bazel/npm by default), one paths entry per package, and treating
	// those as aliases resolved `import 'zod'` to //.bazel/npm/zod/index.d
	// instead of @npm//:zod. A bare "." is the baseUrl root, not a dot-dir.
	return len(head) <= 1 || head[0] != '.' || head == ".."
}

func anyToolManaged(targets []string) bool {
	for _, target := range targets {
		head, _, _ := strings.Cut(aliasTargetPath(target), "/")
		if len(head) > 1 && head[0] == '.' && head != ".." {
			return true
		}
	}
	return false
}

func aliasTargetExists(baseDir, target string) bool {
	rel := aliasTargetPath(target)
	if rel == "" {
		rel = "."
	}
	full := filepath.FromSlash(rel)
	if !filepath.IsAbs(full) {
		full = filepath.Join(baseDir, full)
	}
	if _, err := os.Stat(full); err == nil {
		return true
	}
	if strings.HasSuffix(target, "/*") {
		return false
	}
	for _, ext := range []string{".ts", ".tsx", ".d.ts", ".js"} {
		if _, err := os.Stat(full + ext); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(full, "index"+ext)); err == nil {
			return true
		}
	}
	return false
}

func aliasTargetPath(target string) string {
	return strings.TrimPrefix(strings.TrimSuffix(target, "/*"), "./")
}

// loadNpmMappingFile reads a JSON file that maps npm package names to Bazel
// label strings. The file is expected to have the shape:
//
//	{ "react": "@npm//react", "react-dom": "@npm//react-dom", ... }
func loadNpmMappingFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		// Missing mapping file is not fatal; bare specifiers fall back to the
		// default @npm// convention.
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("typescript: failed to parse npm mapping file %s: %v", path, err)
		return nil
	}
	return m
}

// overlayNpmMapping layers a hand-written npm mapping file over the inventory
// the lockfile produced. The mapping file wins per key, since naming a
// different label for a package is the only thing it is for; every package it
// does not mention keeps the lockfile's answer, which is what stops a mapping
// file listing three overrides from shrinking the inventory to three packages.
//
// The lockfile inventory is shared by pointer across every directory, so this
// copies rather than writing into it -- a ts_npm_mapping directive in one
// subtree must not become the whole workspace's answer.
func overlayNpmMapping(inventory, mapping map[string]string) map[string]string {
	if mapping == nil {
		return inventory
	}
	if inventory == nil {
		return mapping
	}
	merged := make(map[string]string, len(inventory)+len(mapping))
	for name, label := range inventory {
		merged[name] = label
	}
	for name, label := range mapping {
		merged[name] = label
	}
	return merged
}

// ---- Configurer implementation ---------------------------------------------

// configureTsConfig is called by tsLang.Configure for each directory. It
// inherits the parent config, then applies any directives found in the build
// file for the current directory.
func configureTsConfig(c *config.Config, rel string, f *rule.File) {
	// Start with a copy of the parent config (or a fresh one for the root).
	var tc *tsConfig
	if parent, ok := c.Exts[languageName]; ok {
		tc = parent.(*tsConfig).clone()
	} else {
		// Fresh root config: apply defaults.
		tc = &tsConfig{
			packageBoundaryMode: boundaryEveryDir,
			declarations:        "tsgo",
			npmHub:              defaultNpmHub,
		}
	}

	// The lockfile is the workspace's npm inventory, read once and inherited.
	// Not gated on rel == "" the way framework detection is: c.RepoRoot is the
	// workspace root whichever directory Gazelle was pointed at, so a run
	// rooted below it still gets the inventory.
	if !tc.npmInventoryLoaded {
		tc.npmInventoryLoaded = true
		if inventory, lockNames, members := loadNpmInventory(c.RepoRoot); inventory != nil {
			tc.npmPackages = inventory
			tc.npmLockNames = lockNames
			tc.workspaceMembers = members
		}
	}

	// Detect the framework once at the workspace root, then inherit downward.
	// We check rel == "" (root dir) and only run detection when the field has
	// not been set yet (fresh zero value = FrameworkNone and no parent set it).
	if rel == "" && tc.detectedFramework == FrameworkNone {
		tc.detectedFramework = detectFramework(c.RepoRoot)
	}
	if rel == "" && tc.detectedFramework == FrameworkSvelteKit {
		tc.svelteKitAssets, _ = svelteKitAssetsTree(c.RepoRoot)
	}

	// Detect linter config for this directory.
	// linterConfig is inherited from parent dirs via clone(). When a parent
	// already provided a value we only need to check the current directory
	// itself (not walk ancestors again) to avoid O(depth²) stat calls.
	currentDir := filepath.Join(c.RepoRoot, rel)
	if tc.linterConfig != "" {
		// Parent already found a config: check only the current directory for
		// a more-specific override, then keep whatever the parent had.
		if cfgPath, ltype := detectLinterConfigInDir(currentDir, c.RepoRoot); cfgPath != "" && cfgPath != tc.linterConfig {
			tc.linterConfig = cfgPath
			tc.linterType = ltype
		}
	} else {
		// No inherited config: walk from current dir up to the repo root.
		if cfgPath, ltype := detectLinterConfig(c.RepoRoot, currentDir); cfgPath != "" {
			tc.linterConfig = cfgPath
			tc.linterType = ltype
		}
	}

	// Always check for a tsconfig.json in the current directory. When found,
	// read compilerOptions.paths and compilerOptions.baseUrl and use them as
	// the path alias mapping. This is the lower-priority source: the
	// ts_path_alias directives applied below override it.
	tsConfigCandidate := filepath.Join(currentDir, "tsconfig.json")
	if tsConfigAliases := loadTsConfigPaths(tsConfigCandidate, rel); tsConfigAliases != nil {
		tc.pathAliases = tsConfigAliases
		tc.importsAliases = nil
	}
	if _, err := os.Stat(tsConfigCandidate); err == nil {
		// The nearest tsconfig replaces the inherited answer rather than adding
		// to it: tsc gives a file one project, not the union of the projects
		// above it.
		tc.tsconfigAmbientTypes = loadTsConfigAmbientTypes(tsConfigCandidate)
		tc.tsconfigTypes, tc.tsconfigTypeFiles, tc.tsconfigTypeGenerators = loadTsConfigTypeFiles(tsConfigCandidate, rel, f)
		tc.tsconfigTypesDir = rel
	}

	// The compilerOptions baseline, resolved the way tsserver resolves one:
	// nearest file walking up. Inherited from the parent, so the walk only runs
	// where nothing was inherited -- a Gazelle invocation rooted below the
	// workspace root.
	if own := handWrittenTsConfigIn(currentDir, c.RepoRoot); own != "" {
		tc.tsConfigFile = own
	} else if tc.tsConfigFile == "" {
		tc.tsConfigFile = nearestHandWrittenTsConfig(c.RepoRoot, currentDir)
	}

	// An entry here fills a key the sources above left open rather than
	// replacing the answer one of them gave -- except an answer an outer
	// package.json's own map gave, which the nearest enclosing one displaces
	// the way Node resolves a "#".
	if pkgImports := loadPackageImports(currentDir, rel); pkgImports != nil {
		merged := make(map[string]string, len(tc.pathAliases)+len(pkgImports.aliases))
		for key, dir := range tc.pathAliases {
			if outer, fromOuterImports := tc.importsAliases[key]; fromOuterImports && outer == dir {
				continue
			}
			merged[key] = dir
		}
		applied := make(map[string]string, len(pkgImports.aliases))
		for key, dir := range pkgImports.aliases {
			if _, taken := merged[key]; taken {
				continue
			}
			merged[key] = dir
			applied[key] = dir
		}
		tc.pathAliases = merged
		tc.importsAliases = applied
		tc.importsNpm = pkgImports.npm
	}

	// Auto-exclude directories that match the built-in or configured exclude
	// sets. We check the basename of the current directory path so that e.g.
	// "packages/app/dist" is excluded because "dist" is in the built-in set.
	// Once a directory is excluded its children inherit the ignore flag, so
	// we only need to mark the root of the excluded subtree.
	if rel != "" && !tc.ignore {
		dirBasename := filepath.Base(rel)
		if isExcludedDir(dirBasename, tc.excludeDirs) {
			tc.ignore = true
		}
	}

	// Reset per-directory flags that should not propagate past a directory.
	// packageBoundary (the explicit opt-in for a single directory) and
	// targetName are directory-scoped. packageBoundaryMode, ignore,
	// declarations, and the list fields are inherited downward.
	tc.packageBoundary = false
	tc.targetName = ""

	// directivePathAliasSet tracks whether any ts_path_alias directive was
	// seen in this directory's build file. If so, we start with a fresh map
	// (directives replace inherited aliases for clarity) and then populate it
	// from the directives. This flag is local to this invocation.
	var directiveAliases map[string]string

	// Apply directives from the build file.
	if f != nil {
		for _, d := range f.Directives {
			switch d.Key {
			case directivePackageBoundary:
				mode, marksDir, err := boundaryFromDirective(d.Value)
				if err != nil {
					log.Fatalf("typescript: %s: %v", f.Path, err)
				}
				if marksDir {
					// Not a mode: every-dir needs no marker, and setting one
					// there would change what a subtree switching to tsconfig
					// mode below it claims.
					tc.packageBoundary = true
				} else {
					tc.packageBoundaryMode = mode
				}
			case directiveIgnore:
				if d.Value == "false" {
					tc.ignore = false
				} else {
					tc.ignore = true
				}
			case directiveNpmHub:
				tc.npmHub = normalizeNpmHub(d.Value)
			case directiveTargetName:
				tc.targetName = d.Value
			case directiveWarnUnresolved:
				tc.warnUnresolved = d.Value == "true"
			case directiveDeclarations:
				if d.Value == "oxc" || d.Value == "tsgo" {
					tc.declarations = d.Value
				} else {
					log.Printf("gazelle: ts_declarations: expected \"tsgo\" or \"oxc\", got %q; keeping %q", d.Value, tc.declarations)
				}
			case directivePathAlias:
				// # gazelle:ts_path_alias <alias> <dir>
				// On first encounter in this BUILD file, seed the directive map
				// from the inherited aliases so that children can add new keys
				// or override existing ones without losing the parent's aliases.
				// Directives still take priority over tsconfig.json because we
				// always write into directiveAliases and merge it back after
				// the loop.
				if directiveAliases == nil {
					// Seed from inherited aliases so a child can add new keys.
					directiveAliases = make(map[string]string, len(tc.pathAliases))
					for k, v := range tc.pathAliases {
						directiveAliases[k] = v
					}
				}
				parts := strings.SplitN(strings.TrimSpace(d.Value), " ", 2)
				if len(parts) == 2 {
					alias := strings.TrimSpace(parts[0])
					dir := strings.TrimSpace(parts[1])
					if alias != "" && dir != "" {
						directiveAliases[alias] = dir
						tc.aliasesFromDirectives = true
					} else {
						log.Printf("typescript: invalid ts_path_alias value %q (want \"<alias> <dir>\")", d.Value)
					}
				} else {
					log.Printf("typescript: invalid ts_path_alias value %q (want \"<alias> <dir>\")", d.Value)
				}
			case directiveRuntimeDep:
				lbl := strings.TrimSpace(d.Value)
				if lbl != "" {
					tc.runtimeDepsTest = append(tc.runtimeDepsTest, lbl)
				}
			case directiveAmbientTypes:
				lbl := strings.TrimSpace(d.Value)
				if lbl != "" {
					tc.ambientTypes = append(tc.ambientTypes, lbl)
				}
			case directiveExclude:
				tc.addExcludePattern(rel, strings.TrimSpace(d.Value))
			case directiveExcludeDir:
				tc.addExcludeDir(rel, strings.TrimSpace(d.Value))
			case directiveNpmMapping:
				if mappingRel := strings.TrimSpace(d.Value); mappingRel != "" {
					tc.npmPackages = overlayNpmMapping(tc.npmPackages,
						loadNpmMappingFile(filepath.Join(c.RepoRoot, mappingRel)))
				}
			case directiveAssetDeclarationType:
				ext, typeExpr, ok := parseAssetDeclarationTypeDirective(d.Value)
				if !ok {
					break
				}
				if tc.assetDeclarationType == nil {
					tc.assetDeclarationType = map[string]string{}
				}
				tc.assetDeclarationType[ext] = typeExpr
			case directiveJSSrcs:
				if exts, ok := parseJSSrcsDirective(d.Value); ok {
					tc.jsSrcExts = exts
				}
			case directiveCodegen:
				if cp := parseCodegenDirective(rel, d.Value); cp != nil {
					tc.customCodegens = append(tc.customCodegens, *cp)
				} else {
					log.Printf("typescript: invalid ts_codegen directive %q\n"+
						"  format: # gazelle:ts_codegen <name> <generator_label> <outs_csv_or_dir:path> [srcs:<csv>] [args...]", d.Value)
				}
			}
		}
	}

	// If any ts_path_alias directives were present, they replace the
	// path aliases tsconfig.json gave.
	if directiveAliases != nil {
		tc.pathAliases = directiveAliases
	}

	c.Exts[languageName] = tc
}

// ---- directive parser: ts_codegen ------------------------------------------

// splitCodegenSrcs splits a srcs: field on the commas between entries, leaving
// the ones inside a glob() call's own argument list alone.
func splitCodegenSrcs(field string) []string {
	var srcs []string
	depth, start := 0, 0
	flush := func(end int) {
		if src := strings.TrimSpace(field[start:end]); src != "" {
			srcs = append(srcs, src)
		}
	}
	for i, r := range field {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(field))
	return srcs
}

// parseCodegenDirective parses a # gazelle:ts_codegen directive value written
// in rel and returns a CodegenPattern, or nil when the value is malformed.
//
// Format:
//
//	<name> <generator_label> <outs_or_dir> [srcs:<csv>] [args...]
//
// <outs_or_dir> is:
//   - A comma-separated list of output file names, e.g. "api-types.ts"
//     or "types.ts,client.ts".
//   - The prefix "dir:" followed by a directory name, e.g. "dir:generated/client".
//     This sets OutDir instead of Outs (for generators that produce a tree).
//
// An optional "srcs:" field names the generator's inputs, as a comma-separated
// list whose entries are file names or glob() expressions. Omitted, the
// generator reads the TypeScript sources of the directory it was declared in,
// which is what a route-tree or barrel generator wants.
//
// Everything after those fields is treated as positional generator arguments.
//
// Examples:
//
//	api_types @npm//:openapi-typescript_bin api-types.ts srcs:openapi.yaml {srcs} -o {out}
//	prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}
func parseCodegenDirective(rel, value string) *CodegenPattern {
	// Split on whitespace; we need at least 3 fields: name generator outs.
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 3 {
		return nil
	}

	name := fields[0]
	generator := fields[1]
	outsField := fields[2]
	rest := fields[3:] // may be empty

	if name == "" || generator == "" || outsField == "" {
		return nil
	}

	cp := CodegenPattern{
		Name:      name,
		Generator: generator,
		Dir:       rel,
	}

	if len(rest) > 0 && strings.HasPrefix(rest[0], codegenSrcsPrefix) {
		cp.Srcs = splitCodegenSrcs(strings.TrimPrefix(rest[0], codegenSrcsPrefix))
		if len(cp.Srcs) == 0 {
			return nil
		}
		rest = rest[1:]
	}
	cp.Args = rest

	if strings.HasPrefix(outsField, "dir:") {
		cp.OutDir = outsField[len("dir:"):]
		if cp.OutDir == "" {
			return nil
		}
	} else {
		// Comma-separated output file names.
		for _, out := range strings.Split(outsField, ",") {
			out = strings.TrimSpace(out)
			if out != "" {
				cp.Outs = append(cp.Outs, out)
			}
		}
		if len(cp.Outs) == 0 {
			return nil
		}
	}

	return &cp
}

// defaultNpmHub is the repo npm_translate_lock creates when given no name, and
// so the hub a workspace that never sets directiveNpmHub is using.
const defaultNpmHub = "@npm"

// normalizeNpmHub accepts a hub as a repo name or as a repo label, since both
// are how one gets written in a BUILD file, and returns the label form. An
// empty value resets to the default rather than producing "//:react", which
// would silently resolve to a target in the current repo.
func normalizeNpmHub(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "//")
	if value == "" {
		return defaultNpmHub
	}
	if !strings.HasPrefix(value, "@") {
		return "@" + value
	}
	return value
}

// trailingSlash preserves the "/" that path.Join drops, which is what tells a
// wildcard alias apart from an exact one downstream.
func trailingSlash(p string) string {
	if strings.HasSuffix(p, "/") {
		return "/"
	}
	return ""
}

// ---- compilerOptions.types -------------------------------------------------

// loadTsConfigAmbientTypes reads compilerOptions.types out of a tsconfig.json
// and returns the npm labels it names. Returns nil when the file does not exist
// or names nothing resolvable.
//
// An ambient declaration is by definition never imported, so `types` is the
// only place its dependency is written down. Without it the target compiles
// without `ExportedHandler`, `process` or `ImportMetaEnv` and tsgo reports the
// error TypeScript itself would not.
//
// A `types` entry is resolved the way tsc resolves it: a bare name comes from
// typeRoots, so "node" means @types/node, while a scoped or sub-path name is a
// package in its own right ("vite/client" is vite's). An entry that names a
// file in the tree rather than a package resolves to no label at all -- the
// label it would otherwise produce does not parse, and one of those fails the
// whole build rather than the single target that asked for it.
//
// With no `types` key tsc includes every @types package in scope, which under
// pnpm's isolated node_modules is exactly the ones the package.json declares.
func loadTsConfigAmbientTypes(tsConfigPath string) []string {
	data, err := os.ReadFile(tsConfigPath)
	if err != nil {
		return nil
	}
	var tsc tsConfigJSON
	if err := jsonc.Unmarshal(data, &tsc); err != nil {
		return nil
	}
	if tsc.CompilerOptions.Types == nil {
		return declaredTypesPackages(filepath.Join(filepath.Dir(tsConfigPath), "package.json"))
	}
	var labels []string
	seen := make(map[string]struct{})
	for _, entry := range *tsc.CompilerOptions.Types {
		lbl := ambientTypeLabel(entry)
		if lbl == "" {
			continue
		}
		if _, dup := seen[lbl]; dup {
			continue
		}
		seen[lbl] = struct{}{}
		labels = append(labels, lbl)
	}
	return labels
}

// loadTsConfigTypeFiles reads the other half of the same key: the entries that
// name a declaration file in the tsconfig's own directory -- on disk, or
// written there by a ts_worker_types target in the BUILD file -- and, when
// there is at least one, the whole list they are part of.
//
// TypeScript resolves a relative entry against the config the program was
// invoked with, which is the generated one in bazel-out, so an inherited entry
// names nothing. The whole list comes back because `extends` replaces `types`
// whole rather than merging it.
func loadTsConfigTypeFiles(tsConfigPath, rel string, f *rule.File) (entries, files []string, generators map[string]string) {
	data, err := os.ReadFile(tsConfigPath)
	if err != nil {
		return nil, nil, nil
	}
	var tsc tsConfigJSON
	if err := jsonc.Unmarshal(data, &tsc); err != nil {
		return nil, nil, nil
	}
	if tsc.CompilerOptions.Types == nil {
		return nil, nil, nil
	}
	dir := filepath.Dir(tsConfigPath)
	generated := workerTypesOutputs(f)
	seen := make(map[string]struct{})
	for _, entry := range *tsc.CompilerOptions.Types {
		name, isFile := typeEntryFileName(entry)
		if !isFile {
			continue
		}
		if name == "" {
			log.Printf("typescript: the tsconfig in %s names %q in compilerOptions.types, and "+
				"a label can only stage a file of the directory the tsconfig itself is in, so "+
				"nothing below that directory resolves the entry. Move the file next to the "+
				"tsconfig, or write types and types_srcs by hand with a \"# keep\".",
				orRepoRoot(rel), strings.TrimSpace(entry))
			return nil, nil, nil
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		if target, ok := generated[name]; ok {
			if generators == nil {
				generators = make(map[string]string)
			}
			generators[name] = target
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			log.Printf("typescript: the tsconfig in %s names %q in compilerOptions.types and "+
				"no such file is there, so the entry resolves to nothing wherever it is "+
				"written. Fix the entry or drop it.", orRepoRoot(rel), strings.TrimSpace(entry))
			return nil, nil, nil
		}
		files = append(files, name)
	}
	if len(files) == 0 && len(generators) == 0 {
		return nil, nil, nil
	}
	sort.Strings(files)
	for _, entry := range *tsc.CompilerOptions.Types {
		entries = append(entries, strings.TrimSpace(entry))
	}
	return entries, files, generators
}

// workerTypesOutputs is the file each ts_worker_types rule in f writes, keyed by
// its name, with the rule's name as the value. The file is a build output, so
// it is nowhere on disk for os.Stat to find.
func workerTypesOutputs(f *rule.File) map[string]string {
	if f == nil {
		return nil
	}
	var out map[string]string
	for _, r := range f.Rules {
		if r.Kind() != "ts_worker_types" {
			continue
		}
		name := r.AttrString("out")
		if name == "" {
			name = "worker-configuration.d.ts"
		}
		if out == nil {
			out = make(map[string]string)
		}
		out[name] = r.Name()
	}
	return out
}

// typeEntryFileName splits a `types` entry into the declaration file it names
// in the tsconfig's own directory. isFile is false for an entry that names a
// package; the name is "" for a path the tsconfig's own package cannot hold.
//
// The shapes are `types_entry_declaration`'s in ts/private/ts_compile.bzl, the
// half the rule resolves against staged files: one vocabulary, two readers.
func typeEntryFileName(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if !strings.HasPrefix(entry, "./") && !strings.HasPrefix(entry, "../") {
		return "", false
	}
	if !strings.HasSuffix(entry, ".d.ts") {
		return "", false
	}
	name := strings.TrimPrefix(entry, "./")
	if strings.Contains(name, "/") {
		return "", true
	}
	return name, true
}

// ambientTypeLabel converts one compilerOptions.types entry to an @npm label,
// returning "" for an entry that names a file rather than a package.
func ambientTypeLabel(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	if strings.HasPrefix(entry, ".") || strings.HasPrefix(entry, "/") || strings.HasSuffix(entry, ".d.ts") {
		return ""
	}
	if strings.HasPrefix(entry, "@") || strings.Contains(entry, "/") {
		return npmLabel(barePackageName(entry))
	}
	return npmLabel("@types/" + entry)
}

// packageJSONTypeDeps is the sliver of a package.json that says which @types
// packages a directory can see.
type packageJSONTypeDeps struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func declaredTypesPackages(packageJSONPath string) []string {
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return nil
	}
	var pkg packageJSONTypeDeps
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	var names []string
	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for name := range deps {
			if strings.HasPrefix(name, "@types/") {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	labels := make([]string, 0, len(names))
	for _, name := range names {
		labels = append(labels, npmLabel(name))
	}
	return labels
}

// ---- directive parser: ts_asset_declaration_type ---------------------------

// parseAssetDeclarationTypeDirective splits the value into its extension and
// its type expression. Only the first space is a separator, so `{ default: FC }`
// arrives as one expression rather than three fields.
func parseAssetDeclarationTypeDirective(value string) (ext, typeExpr string, ok bool) {
	ext, typeExpr, _ = strings.Cut(strings.TrimSpace(value), " ")
	ext = strings.ToLower(ext)
	typeExpr = strings.TrimSpace(typeExpr)
	if !slices.Contains(assetExtensions, ext) {
		log.Printf("typescript: invalid %s extension %q\n"+
			"  format: # gazelle:%s <ext> <type expression>\n"+
			"  write the leading dot, and pick one of: %s",
			directiveAssetDeclarationType, ext, directiveAssetDeclarationType,
			strings.Join(assetExtensions, ", "))
		return "", "", false
	}
	return ext, typeExpr, true
}

// ---- directive parser: ts_js_srcs ------------------------------------------

// jsSrcExtensions is the closed set ts_js_srcs can name.
//
// Plain .js is absent on purpose. ts_compile declares <stem>.js and <stem>.d.ts
// as the outputs of every .ts src and stages a .js src at its own path
// (ts/private/ts_compile.bzl), so admitting foo.js beside foo.ts would declare
// one file twice and fail analysis. The .d.mts / .d.cts a .mjs / .cjs gets
// instead cannot collide with anything a .ts emits.
var jsSrcExtensions = []string{".mjs", ".cjs"}

// parseJSSrcsDirective reads the extension set a ts_js_srcs directive names.
// The whole set is the value, so a subdirectory naming one extension admits
// that one alone, and naming none admits none.
func parseJSSrcsDirective(value string) ([]string, bool) {
	var exts []string
	for _, field := range strings.Fields(strings.ToLower(value)) {
		ext := field
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if !slices.Contains(jsSrcExtensions, ext) {
			log.Printf("typescript: invalid %s extension %q\n"+
				"  format: # gazelle:%s .mjs .cjs\n"+
				"  pick from: %s -- plain .js is not admissible, since ts_compile\n"+
				"  already declares that name as the output of a .ts of the same stem",
				directiveJSSrcs, field, directiveJSSrcs, strings.Join(jsSrcExtensions, ", "))
			return nil, false
		}
		if !slices.Contains(exts, ext) {
			exts = append(exts, ext)
		}
	}
	return exts, true
}
