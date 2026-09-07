package typescript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/jsonc"
	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
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

	// pathAliases maps an alias prefix ("@/") to a repo-relative directory ("src/"),
	// from the nearest tsconfig's compilerOptions.paths and the nearest package.json's imports.
	pathAliases map[string]string

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

	// tsconfigJsxImportSource is the nearest tsconfig's compilerOptions.jsxImportSource
	// as its extends chain leaves it; "" when no config in the chain names one.
	tsconfigJsxImportSource string

	// tsconfigTypesFiles is each file a path-shaped `types` entry of the nearest
	// tsconfig's chain names, repo-relative; Resolve finds the target staging it.
	tsconfigTypesFiles []string

	// codegenOuts is the ts_codegen declaring each out, by the out's repo-relative path.
	codegenOuts map[string]label.Label

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

	// codegenOutDirs is the workspace-relative root of every out_dir tree a
	// ts_codegen at or above this directory declares, by directive or by rule.
	codegenOutDirs []string

	// programs is the run's tsgo listings, one store every directory shares.
	programs *programStore

	// lock is pnpm-lock.yaml as npm.go reads it, once per run; nil without one.
	lock *npmLock
}

// addCodegenOutDir records one out_dir root; a directive, the detector and the
// rule they wrote name the same tree.
func (tc *tsConfig) addCodegenOutDir(rel, outDir string) {
	if outDir == "" {
		return
	}
	root := path.Join(rel, outDir)
	if root == "." || slices.Contains(tc.codegenOutDirs, root) {
		return
	}
	tc.codegenOutDirs = append(tc.codegenOutDirs, root)
}

// getConfig retrieves the tsConfig from a config.Config. Returns a default
// tsConfig if none has been set yet (i.e. Configure was not called).
func getConfig(c *config.Config) *tsConfig {
	if v, ok := c.Exts[languageName]; ok {
		return v.(*tsConfig)
	}
	return defaultTsConfig()
}

func defaultTsConfig() *tsConfig {
	return &tsConfig{
		packageBoundaryMode: boundaryEveryDir,
		npmHub:              defaultNpmHub,
		programs:            newProgramStore(),
	}
}

// clone returns a copy of the config, suitable for child directories that
// inherit from their parent.
func (tc *tsConfig) clone() *tsConfig {
	cp := *tc
	// Copied: the slices a child appends to and the maps it writes into, so the
	// parent keeps its own. Everything else is replaced whole or meant to be shared.
	if len(tc.excludePatterns) > 0 {
		cp.excludePatterns = make([]excludeRule, len(tc.excludePatterns))
		copy(cp.excludePatterns, tc.excludePatterns)
	}
	if len(tc.ambientTypes) > 0 {
		cp.ambientTypes = make([]string, len(tc.ambientTypes))
		copy(cp.ambientTypes, tc.ambientTypes)
	}
	if len(tc.codegenOuts) > 0 {
		cp.codegenOuts = make(map[string]label.Label, len(tc.codegenOuts))
		for out, codegen := range tc.codegenOuts {
			cp.codegenOuts[out] = codegen
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
	// customCodegens is inherited but not mutated after construction (each
	// directory's directive appends a new entry to the child copy).
	if len(tc.customCodegens) > 0 {
		cp.customCodegens = make([]CodegenPattern, len(tc.customCodegens))
		copy(cp.customCodegens, tc.customCodegens)
	}
	if len(tc.codegenOutDirs) > 0 {
		cp.codegenOutDirs = make([]string, len(tc.codegenOutDirs))
		copy(cp.codegenOutDirs, tc.codegenOutDirs)
	}
	return &cp
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

// loadTsConfigPaths reads the chain's compilerOptions.paths into prefix -> repo-relative
// directory: each entry's first target (tsc's order), rebased through baseUrl and pkgRel.
func loadTsConfigPaths(tsConfigPath, pkgRel string) map[string]string {
	resolved, err := tsconfig.Resolve(tsConfigPath)
	if err != nil || len(resolved.Paths) == 0 {
		return nil
	}

	baseURL := strings.TrimSuffix(resolved.BaseURL, "/")

	// Targets hang off the directory of the config that wrote the value they
	// are relative to, which stops being the leaf as soon as extends is used.
	originDir := resolved.PathsDir
	if baseURL != "" {
		originDir = resolved.BaseURLDir
	}
	originRel := repoRelDir(pkgRel, filepath.Dir(tsConfigPath), originDir)
	if strings.HasPrefix(originRel, "../") {
		log.Printf("typescript: %s inherits paths from %s, outside the repository; "+
			"no label can name that directory, so no import resolves through them.", tsConfigPath, originDir)
		return nil
	}

	// Two patterns can normalise to the same alias key, so iteration order
	// decides which entry survives, and which order the log lines come out in.
	patterns := make([]string, 0, len(resolved.Paths))
	for aliasPattern := range resolved.Paths {
		patterns = append(patterns, aliasPattern)
	}
	sort.Strings(patterns)

	aliases := make(map[string]string, len(resolved.Paths))
	for _, aliasPattern := range patterns {
		targets := resolved.Paths[aliasPattern]
		if len(targets) == 0 {
			continue
		}
		target := targets[0]

		// Strip trailing "/*" wildcard from both sides.
		aliasKey := strings.TrimSuffix(aliasPattern, "/*")
		targetDir := strings.TrimSuffix(target, "/*")

		// Strip leading "./" from the target.
		targetDir = strings.TrimPrefix(targetDir, "./")

		// Prepend baseUrl when set and target is not absolute.
		if baseURL != "" && !strings.HasPrefix(targetDir, "/") {
			targetDir = path.Join(baseURL, targetDir)
		}

		// An identity mapping (the editor tsconfig's first-party entries) tells the
		// resolver nothing the package path does not.
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
		tc = defaultTsConfig()
	}

	// The lockfile is the workspace's npm inventory, read once and inherited.
	// Not gated on rel == "": c.RepoRoot is the workspace root whichever
	// directory Gazelle was pointed at, so a run rooted below it still gets the
	// inventory.
	if !tc.npmInventoryLoaded {
		tc.npmInventoryLoaded = true
		if inventory, lockNames, members := loadNpmInventory(c.RepoRoot); inventory != nil {
			tc.npmPackages = inventory
			tc.npmLockNames = lockNames
			tc.workspaceMembers = members
		}
		switch l, err := loadNpmLock(c.RepoRoot); {
		case err == nil:
			tc.lock = l
		case !errors.Is(err, fs.ErrNotExist):
			log.Fatalf("typescript: %s: %v", pnpmLockfileName, err)
		}
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

	// The nearest tsconfig.json's paths are the alias map below it.
	tsConfigCandidate := filepath.Join(currentDir, "tsconfig.json")
	if tsConfigAliases := loadTsConfigPaths(tsConfigCandidate, rel); tsConfigAliases != nil {
		tc.pathAliases = tsConfigAliases
		tc.importsAliases = nil
	}
	tc.recordCodegenOuts(c.RepoName, rel, f)
	if _, err := os.Stat(tsConfigCandidate); err == nil {
		// The nearest tsconfig replaces the inherited answer rather than adding
		// to it: tsc gives a file one project, not the union of the projects
		// above it.
		tc.tsconfigAmbientTypes = loadTsConfigAmbientTypes(tsConfigCandidate)
		tc.tsconfigJsxImportSource = loadTsConfigJsxImportSource(tsConfigCandidate)
		tc.tsconfigTypesFiles = typesEntryFiles(tsConfigCandidate, rel)
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

	// packageBoundary and targetName are directory-scoped; every other field is
	// inherited downward.
	tc.packageBoundary = false
	tc.targetName = ""

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
		for _, r := range f.Rules {
			if r.Kind() == "ts_codegen" {
				tc.addCodegenOutDir(rel, r.AttrString("out_dir"))
			}
		}
	}

	// The directories below this one are visited before generateRules runs
	// here, so a detected generator's out_dir has to be recorded now.
	if !tc.ignore {
		for _, cp := range detectCodegen(rel, detectorInputs(currentDir, f), tc) {
			tc.addCodegenOutDir(rel, cp.OutDir)
		}
	}
	if !tc.ignore && handWrittenTsConfigIn(currentDir, c.RepoRoot) != "" {
		listTsConfigProgram(c.RepoRoot, rel, tc)
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
	resolved, err := tsconfig.Resolve(tsConfigPath)
	if err != nil {
		return nil
	}
	if resolved.Types == nil {
		return declaredTypesPackages(filepath.Join(filepath.Dir(tsConfigPath), "package.json"))
	}
	var labels []string
	seen := make(map[string]struct{})
	for _, entry := range *resolved.Types {
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

// codegenDeclarationOutputs maps each .d.ts a ts_codegen in f declares in outs to
// the rule's name: a build output, so nowhere on disk for os.Stat to find.
func codegenDeclarationOutputs(f *rule.File) map[string]string {
	if f == nil {
		return nil
	}
	var out map[string]string
	for _, r := range f.Rules {
		if r.Kind() != "ts_codegen" {
			continue
		}
		for _, name := range r.AttrStrings("outs") {
			if !isDeclarationFile(name) || strings.Contains(name, "/") {
				continue
			}
			if out == nil {
				out = make(map[string]string)
			}
			out[name] = r.Name()
		}
	}
	return out
}

// recordCodegenOuts indexes the outs of every ts_codegen in f by repo-relative path.
func (tc *tsConfig) recordCodegenOuts(repoName, rel string, f *rule.File) {
	if f == nil {
		return
	}
	for _, r := range f.Rules {
		if r.Kind() != "ts_codegen" {
			continue
		}
		for _, out := range r.AttrStrings("outs") {
			if tc.codegenOuts == nil {
				tc.codegenOuts = make(map[string]label.Label)
			}
			tc.codegenOuts[path.Join(rel, out)] = label.New(repoName, rel, r.Name())
		}
	}
}

// The files the chain's path-shaped `types` entries name, repo-relative: tsc
// resolves an inherited entry against the program's directory, not its setter's.
func typesEntryFiles(tsConfigPath, rel string) []string {
	resolved, err := tsconfig.Resolve(tsConfigPath)
	if err != nil || resolved.Types == nil {
		return nil
	}
	var files []string
	for _, entry := range *resolved.Types {
		entry = strings.TrimSpace(entry)
		if !strings.HasPrefix(entry, "./") && !strings.HasPrefix(entry, "../") {
			continue
		}
		if file := path.Join(rel, entry); !slices.Contains(files, file) {
			files = append(files, file)
		}
	}
	return files
}

// ambientTypePackage is the package one compilerOptions.types entry names, ""
// for an entry that names a file rather than a package.
func ambientTypePackage(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	if strings.HasPrefix(entry, ".") || strings.HasPrefix(entry, "/") || isDeclarationFile(entry) {
		return ""
	}
	if strings.HasPrefix(entry, "@") || strings.Contains(entry, "/") {
		return barePackageName(entry)
	}
	return "@types/" + entry
}

// ambientTypeLabel converts one compilerOptions.types entry to an @npm label,
// returning "" for an entry that names a file rather than a package.
func ambientTypeLabel(entry string) string {
	pkg := ambientTypePackage(entry)
	if pkg == "" {
		return ""
	}
	return npmLabel(pkg)
}

// npmLabel converts an npm package name to its @npm//:<label> form.
func npmLabel(pkgName string) string {
	return "@npm//:" + npmPackageToLabelName(pkgName)
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
