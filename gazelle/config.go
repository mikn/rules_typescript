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

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/jsonc"
	"github.com/mikn/rules_typescript/ts/tools/tsconfig"
)

// tsConfig is one directory's configuration, cloned down the tree; programs
// and lock are the run's, shared by every directory.
type tsConfig struct {
	protos          *protoStore
	protoIdentities map[string]*protoIdentity
	protoEnabled    []string
	// codegenOuts is the ts_codegen declaring each out, by repo-relative path;
	// codegenOutDirs every out_dir root a ts_codegen at or above declares.
	codegenOuts    map[string]label.Label
	codegenOutDirs []string
	// The nearest package.json above when the lockfile has no importer for it.
	foreignManifest string

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
	return &tsConfig{programs: newProgramStore(), protos: newProtoStore(), protoIdentities: map[string]*protoIdentity{}}
}

// clone returns a copy of the config, suitable for child directories that
// inherit from their parent.
func (tc *tsConfig) clone() *tsConfig {
	cp := *tc
	cp.protoIdentities = maps.Clone(tc.protoIdentities)
	cp.protoEnabled = slices.Clone(tc.protoEnabled)
	// Copied: what a child writes into, so the parent keeps its own.
	if len(tc.codegenOuts) > 0 {
		cp.codegenOuts = maps.Clone(tc.codegenOuts)
	}
	if len(tc.codegenOutDirs) > 0 {
		cp.codegenOutDirs = slices.Clone(tc.codegenOutDirs)
	}
	return &cp
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
// cloned, the codegens declared here, the program listed.
func configureTsConfig(c *config.Config, rel string, f *rule.File) {
	var tc *tsConfig
	if parent, ok := c.Exts[languageName]; ok {
		tc = parent.(*tsConfig).clone()
	} else {
		tc = defaultTsConfig()
	}

	if err := configureProto(c, rel, f, tc); err != nil {
		log.Fatalf("typescript: %v", err)
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

	tc.programs.recordEmissionConsumers(c.RepoRoot, rel, f)
	tc.programs.emission.lock = tc.lock
	currentDir := filepath.Join(c.RepoRoot, rel)
	// pnpm installs nothing for a package.json the lockfile has no importer
	// for, so the project under it is foreign; an importer below ends that.
	if hasPackageJSON(currentDir) {
		tc.foreignManifest = ""
		if tc.lock != nil && tc.lock.importers[rel] == nil {
			tc.foreignManifest = path.Join(rel, "package.json")
		}
	}
	if tc.foreignManifest != "" {
		tc.programs.foreign[rel] = tc.foreignManifest
	}
	tc.recordCodegens(c.RepoName, rel, f)
	if handWrittenTsConfigIn(currentDir, c.RepoRoot) != "" {
		listTsConfigProgram(c.RepoRoot, rel, tc)
	} else if hasPackageJSON(currentDir) {
		listManifestProgram(c.RepoRoot, rel, tc)
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
