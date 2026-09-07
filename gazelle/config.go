package typescript

import (
	"errors"
	"io/fs"
	"log"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/jsonc"
	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// tsConfig is one directory's configuration, cloned down the tree; programs
// and lock are the run's, shared by every directory.
type tsConfig struct {
	// The nearest linter config at or above this directory, repo-relative,
	// and its kind, "oxlint" or "eslint"; "" when none is in force.
	linterConfig string
	linterType   string

	// codegenOuts is the ts_codegen declaring each out, by repo-relative path;
	// codegenOutDirs every out_dir root a ts_codegen at or above declares.
	codegenOuts    map[string]label.Label
	codegenOutDirs []string

	programs   *programStore
	lock       *npmLock
	lockLoaded bool
}

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
	return &tsConfig{programs: newProgramStore()}
}

// clone returns a copy of the config, suitable for child directories that
// inherit from their parent.
func (tc *tsConfig) clone() *tsConfig {
	cp := *tc
	// Copied: what a child writes into, so the parent keeps its own.
	if len(tc.codegenOuts) > 0 {
		cp.codegenOuts = maps.Clone(tc.codegenOuts)
	}
	if len(tc.codegenOutDirs) > 0 {
		cp.codegenOutDirs = slices.Clone(tc.codegenOutDirs)
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

// linterBinaryLabel is the hub's bin alias for the linter package; linterType
// is the package name.
func linterBinaryLabel(tc *tsConfig) string {
	return "@npm//:" + npmPackageToLabelName(tc.linterType) + "_bin"
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

// ---- the tsconfig.json -----------------------------------------------------

// tsConfigTargetName is the ts_config target Gazelle writes beside a package's
// own tsconfig.json, and the one a target under it names.
const tsConfigTargetName = "tsconfig"

// generatedTsConfigMarker is the _comment ts_refresh_tsconfig stamps on every
// tsconfig.json it writes (ts/private/tsconfig_aspect.bzl, _HEADER).
const generatedTsConfigMarker = "bazel run //:refresh_tsconfig"

// A tsconfig.json this ruleset writes is built out of the very targets that
// would name it, so it is never a program and never a ts_config's src.
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

// ---- Configurer implementation ---------------------------------------------

// configureTsConfig is tsLang.Configure for one directory: the parent's config
// cloned, the linter in force, the codegens declared here, the program listed.
func configureTsConfig(c *config.Config, rel string, f *rule.File) {
	var tc *tsConfig
	if parent, ok := c.Exts[languageName]; ok {
		tc = parent.(*tsConfig).clone()
	} else {
		tc = defaultTsConfig()
	}

	// c.RepoRoot is the workspace root whichever directory Gazelle was pointed
	// at, so a run rooted below it still reads the lockfile, once.
	if !tc.lockLoaded {
		tc.lockLoaded = true
		switch l, err := loadNpmLock(c.RepoRoot); {
		case err == nil:
			tc.lock = l
		case !errors.Is(err, fs.ErrNotExist):
			log.Fatalf("typescript: %s: %v", pnpmLockfileName, err)
		}
	}

	// The linter config is inherited, so the ancestor walk runs only where
	// nothing was inherited: a run rooted below the workspace root.
	currentDir := filepath.Join(c.RepoRoot, rel)
	if tc.linterConfig != "" {
		if cfgPath, ltype := detectLinterConfigInDir(currentDir, c.RepoRoot); cfgPath != "" && cfgPath != tc.linterConfig {
			tc.linterConfig = cfgPath
			tc.linterType = ltype
		}
	} else {
		cfgPath, ltype := detectLinterConfig(c.RepoRoot, currentDir)
		if cfgPath != "" {
			tc.linterConfig = cfgPath
			tc.linterType = ltype
		}
	}

	tc.recordCodegens(c.RepoName, rel, f)
	if handWrittenTsConfigIn(currentDir, c.RepoRoot) != "" {
		listTsConfigProgram(c.RepoRoot, rel, tc)
	}

	c.Exts[languageName] = tc
}

// recordCodegens indexes every ts_codegen in f: its outs by repo-relative path
// and its out_dir root, both read before the directories below generate.
func (tc *tsConfig) recordCodegens(repoName, rel string, f *rule.File) {
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
		tc.addCodegenOutDir(rel, r.AttrString("out_dir"))
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
