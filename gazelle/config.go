package typescript

import (
	"encoding/json"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/rule"
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
	// Without a value (or with "every-dir") every directory that contains .ts
	// files gets a ts_compile target (the new default, matching Go's behaviour).
	// With the value "index-only" the old behaviour is restored: only
	// directories that contain an index.ts/tsx file are treated as boundaries.
	//   # gazelle:ts_package_boundary            → every-dir (default)
	//   # gazelle:ts_package_boundary every-dir  → same as above
	//   # gazelle:ts_package_boundary index-only → restore old behaviour
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

	// directiveIsolatedDeclarations controls whether the generated ts_compile
	// rules set isolated_declarations = False. The default (true) means no
	// attribute is emitted (the rule default is true). Set to "false" to
	// generate ts_compile rules with isolated_declarations = False throughout
	// the directory tree.
	//   # gazelle:ts_isolated_declarations false
	directiveIsolatedDeclarations = "ts_isolated_declarations"

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

	// directiveExclude registers an additional file glob pattern to exclude
	// from source targets. The value is a filepath.Match-style pattern matched
	// against the file basename.
	//   # gazelle:ts_exclude *.generated.ts
	directiveExclude = "ts_exclude"

	// directiveCodegen registers a custom ts_codegen target via a directive.
	// Format: # gazelle:ts_codegen <name> <generator_label> <outs_csv> [args...]
	// The <outs_csv> field is a comma-separated list of output file names.
	// Everything after <outs_csv> is treated as generator args.
	//
	// Example (single output, args with placeholder substitution):
	//   # gazelle:ts_codegen api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}
	//
	// Example (directory output via out_dir prefix):
	//   # gazelle:ts_codegen prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}
	//
	// When the <outs_csv> value starts with "dir:" the remainder is treated as
	// the out_dir value and the Outs slice is left empty.
	directiveCodegen = "ts_codegen"
)

// packageBoundaryMode values.
const (
	// boundaryEveryDir is the default: every directory with .ts files gets a
	// ts_compile target.
	boundaryEveryDir = "every-dir"

	// boundaryIndexOnly restores the old behaviour: only directories that
	// contain an index.ts/tsx file are treated as package boundaries.
	boundaryIndexOnly = "index-only"
)

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

	// packageBoundaryMode controls how package boundaries are detected.
	// "every-dir" (default): every directory with .ts files gets a ts_compile.
	// "index-only": only directories with index.ts/tsx (old behaviour).
	//
	// Note: when packageBoundaryMode == boundaryEveryDir this field replaces the
	// old boolean packageBoundary field. The old bool is kept for the
	// index-only mode so that # gazelle:ts_package_boundary (no value) still
	// marks an explicit boundary in index-only mode.
	packageBoundaryMode string

	// packageBoundary indicates that this specific directory is an explicit
	// package boundary. Used only when packageBoundaryMode == boundaryIndexOnly
	// to allow individual directories to opt-in regardless of index.ts.
	packageBoundary bool

	// ignore suppresses ts_compile / ts_test generation in this directory.
	ignore bool

	// targetName overrides the default target name (which is the directory
	// basename). Empty means use the default.
	targetName string

	// pathAliases maps a TypeScript path alias prefix (e.g. "@/") to a
	// workspace-relative directory path (e.g. "src/"). Can be populated from
	// tsconfig.json, gazelle_ts.json (deprecated), or # gazelle:ts_path_alias
	// directives. Directives take priority over file-based sources.
	pathAliases map[string]string

	// npmPackages holds the set of npm package names known to the workspace.
	// Keys are npm package names (e.g. "react"); values are the Bazel label
	// string to use as a dep (e.g. "@npm//react").
	// Populated once from the npm mapping file and then shared across all
	// directories via pointer-equality (never mutated after load).
	npmPackages map[string]string

	// warnUnresolved controls whether a warning is emitted for imports that
	// cannot be resolved to any Bazel label. When false (the default) such
	// imports are silently skipped. Enable via:
	//   # gazelle:ts_warn_unresolved true
	warnUnresolved bool

	// excludePatterns holds additional file glob patterns (basenames only) to
	// exclude from source targets beyond the built-in generated-file rules.
	// Can be populated from gazelle_ts.json (deprecated) or
	// # gazelle:ts_exclude directives. Directives append to the inherited list.
	// Each entry is a simple pattern matched against the file basename using
	// filepath.Match semantics.
	excludePatterns []string

	// excludeDirs holds directory basenames that should be excluded from
	// Gazelle traversal. Loaded from the "excludeDirs" key in gazelle_ts.json
	// (deprecated). The built-in set (.next, .nuxt, .svelte-kit, dist, build)
	// is always excluded regardless of this setting.
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

	// runtimeDepsTest is the list of additional Bazel label strings that
	// should be appended to every generated ts_test deps list. Can be
	// populated from gazelle_ts.json (deprecated) or
	// # gazelle:ts_runtime_dep directives. Directives append to the list.
	// Use this for packages that are needed at test runtime but are never
	// statically imported (e.g. "happy-dom", "@vitest/coverage-v8").
	runtimeDepsTest []string

	// isolatedDeclarations controls whether the generated ts_compile rules
	// set isolated_declarations = False. The default (true) means no
	// attribute is emitted. Set to false via # gazelle:ts_isolated_declarations
	// false to generate ts_compile rules with isolated_declarations = False.
	isolatedDeclarations bool

	// customCodegens holds ts_codegen patterns parsed from
	// # gazelle:ts_codegen directives. Each directive contributes one entry.
	// Format: # gazelle:ts_codegen <name> <generator_label> <outs_csv> [args...]
	// Example: # gazelle:ts_codegen api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}
	// These patterns are appended verbatim to whatever detectCodegen returns.
	customCodegens []CodegenPattern
}

// getConfig retrieves the tsConfig from a config.Config. Returns a default
// tsConfig if none has been set yet (i.e. Configure was not called).
func getConfig(c *config.Config) *tsConfig {
	if v, ok := c.Exts[languageName]; ok {
		return v.(*tsConfig)
	}
	return &tsConfig{
		packageBoundaryMode:  boundaryEveryDir,
		isolatedDeclarations: true,
	}
}

// clone returns a copy of the config, suitable for child directories that
// inherit from their parent.
func (tc *tsConfig) clone() *tsConfig {
	cp := *tc
	// npmPackages is read-only after construction; sharing via pointer is safe.
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
		cp.excludePatterns = make([]string, len(tc.excludePatterns))
		copy(cp.excludePatterns, tc.excludePatterns)
	}
	if len(tc.runtimeDepsTest) > 0 {
		cp.runtimeDepsTest = make([]string, len(tc.runtimeDepsTest))
		copy(cp.runtimeDepsTest, tc.runtimeDepsTest)
	}
	// customCodegens is inherited but not mutated after construction (each
	// directory's directive appends a new entry to the child copy).
	if len(tc.customCodegens) > 0 {
		cp.customCodegens = make([]CodegenPattern, len(tc.customCodegens))
		copy(cp.customCodegens, tc.customCodegens)
	}
	return &cp
}

// ---- gazelle_ts.json -------------------------------------------------------

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

// linterBinaryLabel returns the conventional Bazel label for the linter
// binary based on linterType. Returns empty string when linterType is empty.
// Users can override this in their BUILD files.
func linterBinaryLabel(linterType string) string {
	switch linterType {
	case "oxlint":
		return "@npm//:oxlint_bin"
	case "eslint":
		return "@npm//:eslint_bin"
	default:
		return ""
	}
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

// ---- tsconfig.json reading -------------------------------------------------

// tsConfigJSON is a minimal representation of tsconfig.json used only for
// reading compilerOptions.paths and compilerOptions.baseUrl.
type tsConfigJSON struct {
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// loadTsConfigPaths reads compilerOptions.paths and compilerOptions.baseUrl
// from a tsconfig.json file. The baseUrl (if present) is used to resolve the
// target directories in the paths entries. Returns nil when the file does not
// exist or has no paths.
//
// The paths format in tsconfig is:
//
//	"@/*": ["src/*"]
//	"@components/*": ["src/components/*"]
//
// We convert each path pattern to the simpler prefix→dir form used by tsConfig.pathAliases:
//   - Strip trailing "/*" from both the alias key and the first target value.
//   - Use the first target in the array (tsconfig supports fallback arrays; we only need one).
//   - Prepend baseUrl to the target directory when baseUrl is non-empty.
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
func loadTsConfigPaths(tsConfigPath string) map[string]string {
	data, err := os.ReadFile(tsConfigPath)
	if err != nil {
		return nil
	}
	var tsc tsConfigJSON
	if err := json.Unmarshal(data, &tsc); err != nil {
		log.Printf("typescript: failed to parse %s: %v", tsConfigPath, err)
		return nil
	}
	if len(tsc.CompilerOptions.Paths) == 0 {
		return nil
	}

	baseURL := strings.TrimSuffix(tsc.CompilerOptions.BaseURL, "/")

	aliases := make(map[string]string, len(tsc.CompilerOptions.Paths))
	for aliasPattern, targets := range tsc.CompilerOptions.Paths {
		if len(targets) == 0 {
			continue
		}
		if len(targets) > 1 {
			log.Printf("typescript: paths entry %q has %d targets; using only %q (first)", aliasPattern, len(targets), targets[0])
		}
		target := targets[0] // use first fallback entry only

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

		// Ensure the alias key ends with "/" only when it was a wildcard pattern.
		if strings.HasSuffix(aliasPattern, "/*") && !strings.HasSuffix(aliasKey, "/") {
			aliasKey = aliasKey + "/"
		}
		// Ensure the target dir ends with "/" when the alias has a wildcard.
		if strings.HasSuffix(aliasPattern, "/*") && !strings.HasSuffix(targetDir, "/") {
			targetDir = targetDir + "/"
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

// ---- gazelle_ts.json -------------------------------------------------------

// gazelleTs is the schema for gazelle_ts.json, a per-repo configuration file
// that provides TypeScript path aliases and npm package mapping overrides.
//
// Example gazelle_ts.json:
//
//	{
//	  "pathAliases": {
//	    "@/": "src/",
//	    "@components/": "src/components/"
//	  },
//	  "npmMappingFile": "npm/package_mapping.json",
//	  "excludePatterns": ["*.generated.ts", "*.auto.ts"],
//	  "excludeDirs": ["coverage", "storybook-static"],
//	  "runtimeDeps": {
//	    "test": ["@npm//:happy-dom", "@npm//:react", "@npm//:react-dom"]
//	  }
//	}
type gazelleTs struct {
	// PathAliases maps TypeScript path alias prefixes to workspace-relative
	// directory prefixes. Keys should end with "/" if they represent directory
	// aliases.
	PathAliases map[string]string `json:"pathAliases"`

	// NpmMappingFile is an optional path (relative to the workspace root) to a
	// JSON file that maps npm package names to Bazel label strings. When absent,
	// bare-specifier imports are resolved using the default @npm// convention.
	NpmMappingFile string `json:"npmMappingFile"`

	// ExcludePatterns is a list of file glob patterns (matched against file
	// basenames) to exclude from source targets in addition to the built-in
	// generated-file exclusions (*.gen.ts, *.generated.ts, *.auto.ts).
	// Patterns use filepath.Match semantics.
	ExcludePatterns []string `json:"excludePatterns"`

	// ExcludeDirs is a list of directory basenames to exclude from Gazelle
	// traversal in addition to the built-in set (.next, .nuxt, .svelte-kit,
	// dist, build).
	ExcludeDirs []string `json:"excludeDirs"`

	// RuntimeDeps contains Bazel labels that are appended to generated targets
	// even when they are never statically imported. Each key is a target kind:
	//   "test"    — labels appended to every ts_test deps list
	// Use "test" for packages needed at test runtime without a static import:
	// happy-dom, @vitest/coverage-v8, react (JSX runtime), etc.
	RuntimeDeps struct {
		Test []string `json:"test"`
	} `json:"runtimeDeps"`
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
			packageBoundaryMode:  boundaryEveryDir,
			isolatedDeclarations: true,
		}
	}

	// Detect the framework once at the workspace root, then inherit downward.
	// We check rel == "" (root dir) and only run detection when the field has
	// not been set yet (fresh zero value = FrameworkNone and no parent set it).
	if rel == "" && tc.detectedFramework == FrameworkNone {
		tc.detectedFramework = detectFramework(c.RepoRoot)
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
	// the path alias mapping. This is the lower-priority source: gazelle_ts.json
	// (loaded below) overrides tsconfig.json when both are present.
	tsConfigCandidate := filepath.Join(currentDir, "tsconfig.json")
	if tsConfigAliases := loadTsConfigPaths(tsConfigCandidate); tsConfigAliases != nil {
		tc.pathAliases = tsConfigAliases
	}

	// Always check for a gazelle_ts.json in the current directory. A local
	// file overrides any settings inherited from a parent directory, allowing
	// sub-trees to carry their own path aliases and npm package mappings.
	// gazelle_ts.json takes priority over tsconfig.json for path aliases but
	// is lower-priority than # gazelle: directives (applied below).
	//
	// gazelle_ts.json is deprecated. Users should migrate to directives.
	candidate := filepath.Join(currentDir, "gazelle_ts.json")
	var gazelleJSON *gazelleTs
	if data, err := os.ReadFile(candidate); err == nil {
		log.Printf("typescript: gazelle_ts.json at %s is deprecated.\n"+
			"Migrate to # gazelle: directives in your root BUILD.bazel:\n"+
			"  pathAliases      → # gazelle:ts_path_alias @/ src/\n"+
			"  excludePatterns  → # gazelle:ts_exclude *.generated.ts\n"+
			"  runtimeDeps.test → # gazelle:ts_runtime_dep @npm//:happy-dom",
			candidate)
		var gtsCfg gazelleTs
		if jsonErr := json.Unmarshal(data, &gtsCfg); jsonErr != nil {
			log.Printf("typescript: failed to parse %s: %v", candidate, jsonErr)
		} else {
			gazelleJSON = &gtsCfg
			if gtsCfg.PathAliases != nil {
				// gazelle_ts.json explicitly sets pathAliases — this takes
				// priority over any tsconfig.json paths we read above, but
				// directives (applied below) take priority over this.
				tc.pathAliases = gtsCfg.PathAliases
			}
			if gtsCfg.NpmMappingFile != "" {
				npmPath := filepath.Join(c.RepoRoot, gtsCfg.NpmMappingFile)
				tc.npmPackages = loadNpmMappingFile(npmPath)
			}
			if len(gtsCfg.ExcludePatterns) > 0 {
				tc.excludePatterns = gtsCfg.ExcludePatterns
			}
			if len(gtsCfg.ExcludeDirs) > 0 {
				tc.excludeDirs = gtsCfg.ExcludeDirs
			}
			if len(gtsCfg.RuntimeDeps.Test) > 0 {
				tc.runtimeDepsTest = gtsCfg.RuntimeDeps.Test
			}
		}
	}
	_ = gazelleJSON // used for backwards compat above; directives below take priority

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
	// packageBoundary (explicit opt-in for a single dir in index-only mode)
	// and targetName are directory-scoped. packageBoundaryMode, ignore,
	// isolatedDeclarations, and the list fields are inherited downward.
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
				// Values: "" / "every-dir" → every-dir mode
				//         "index-only"     → index-only mode
				//         "true"           → explicit per-directory boundary marker
				//                            (only meaningful in index-only mode)
				//
				// In every-dir mode the packageBoundary flag is NOT set because
				// every directory with .ts files is already a boundary; setting
				// the flag would create confusing side-effects when the mode is
				// later switched to index-only in a sub-tree.
				// In index-only mode, "true" allows a single directory to opt-in
				// as a boundary even without an index.ts file.
				switch strings.TrimSpace(d.Value) {
				case "", "every-dir":
					tc.packageBoundaryMode = boundaryEveryDir
					// Do NOT set packageBoundary; every-dir mode doesn't need it.
				case "index-only":
					tc.packageBoundaryMode = boundaryIndexOnly
				case "true":
					// Explicit per-directory opt-in for index-only mode.
					tc.packageBoundary = true
				default:
					log.Printf("typescript: unknown ts_package_boundary value %q (want every-dir, index-only, or true)", d.Value)
				}
			case directiveIgnore:
				if d.Value == "false" {
					tc.ignore = false
				} else {
					tc.ignore = true
				}
			case directiveTargetName:
				tc.targetName = d.Value
			case directiveWarnUnresolved:
				tc.warnUnresolved = d.Value == "true"
			case directiveIsolatedDeclarations:
				tc.isolatedDeclarations = d.Value != "false"
			case directivePathAlias:
				// # gazelle:ts_path_alias <alias> <dir>
				// On first encounter in this BUILD file, seed the directive map
				// from the inherited aliases so that children can add new keys
				// or override existing ones without losing the parent's aliases.
				// Directives still take priority over file-based sources
				// (tsconfig.json / gazelle_ts.json) because we always write
				// into directiveAliases and merge it back after the loop.
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
			case directiveExclude:
				pattern := strings.TrimSpace(d.Value)
				if pattern != "" {
					tc.excludePatterns = append(tc.excludePatterns, pattern)
				}
			case directiveCodegen:
				// Format: <name> <generator_label> <outs_or_dir> [args...]
				// <outs_or_dir> is either:
				//   - a comma-separated list of output file names, or
				//   - "dir:<directory_name>" for directory-tree outputs.
				if cp := parseCodegenDirective(d.Value); cp != nil {
					tc.customCodegens = append(tc.customCodegens, *cp)
				} else {
					log.Printf("typescript: invalid ts_codegen directive %q\n"+
						"  format: # gazelle:ts_codegen <name> <generator_label> <outs_csv_or_dir:path> [args...]", d.Value)
				}
			}
		}
	}

	// If any ts_path_alias directives were present, they replace the
	// file-based path aliases (tsconfig.json / gazelle_ts.json).
	if directiveAliases != nil {
		tc.pathAliases = directiveAliases
	}

	c.Exts[languageName] = tc
}

// ---- directive parser: ts_codegen ------------------------------------------

// parseCodegenDirective parses a # gazelle:ts_codegen directive value and
// returns a CodegenPattern, or nil when the value is malformed.
//
// Format:
//
//	<name> <generator_label> <outs_or_dir> [args...]
//
// <outs_or_dir> is:
//   - A comma-separated list of output file names, e.g. "api-types.ts"
//     or "types.ts,client.ts".
//   - The prefix "dir:" followed by a directory name, e.g. "dir:generated/client".
//     This sets OutDir instead of Outs (for generators that produce a tree).
//
// Everything after <outs_or_dir> is treated as positional generator arguments.
//
// Examples:
//
//	api_types @npm//:openapi-typescript_bin api-types.ts {srcs} -o {out}
//	prisma_client @npm//:prisma_bin dir:generated/client generate --schema {srcs}
func parseCodegenDirective(value string) *CodegenPattern {
	// Split on whitespace; we need at least 3 fields: name generator outs.
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 3 {
		return nil
	}

	name := fields[0]
	generator := fields[1]
	outsField := fields[2]
	args := fields[3:] // may be empty

	if name == "" || generator == "" || outsField == "" {
		return nil
	}

	cp := CodegenPattern{
		Name:      name,
		Generator: generator,
		Args:      args,
	}

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
