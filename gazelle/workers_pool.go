package typescript

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

// The Workers pool's half of Gazelle, the one tool-specific file: called from
// generate.go and resolve.go; docs/gazelle/overview.md § A Workers-Pool Config.

const (
	wranglerConfigTargetName = "wrangler_config"
	workersPoolPackage       = "@cloudflare/vitest-pool-workers"
	istanbulPackage          = "@vitest/coverage-istanbul"
)

// A string literal naming a wrangler config: `wrangler.configPath` as written.
var wranglerLiteral = regexp.MustCompile(
	"[\"'`]([^\"'`]*wrangler[\\w.-]*\\.(?:jsonc|json|toml))[\"'`]")

// wranglerConfigOf is the wrangler config the vitest config at cfg names in a
// literal, repo-relative; "" and one line for none, two, or one outside cfg's.
func (s *programStore) wranglerConfigOf(repoRoot, cfg string) string {
	if file, done := s.wranglerConfigs[cfg]; done {
		return file
	}
	file := ""
	switch names := wranglerLiterals(filepath.Join(repoRoot, cfg)); {
	case len(names) == 0:
	case len(names) > 1:
		log.Printf("typescript: %s names %d wrangler configs (%s), so nothing "+
			"is staged as wrangler_config", cfg, len(names),
			strings.Join(names, ", "))
	default:
		file = s.wranglerConfigIn(repoRoot, cfg, names[0])
	}
	s.wranglerConfigs[cfg] = file
	return file
}

// wranglerLiterals is every distinct wrangler config the file names, each as
// first spelled.
func wranglerLiterals(file string) []string {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var out, seen []string
	for _, m := range wranglerLiteral.FindAllSubmatch(data, -1) {
		lit := string(m[1])
		if clean := path.Clean(lit); !slices.Contains(seen, clean) {
			seen = append(seen, clean)
			out = append(out, lit)
		}
	}
	return out
}

// The named file when it is in the config's package and on disk.
func (s *programStore) wranglerConfigIn(repoRoot, cfg, literal string) string {
	dir := parentDir(cfg)
	file := path.Join(dir, literal)
	if !firstParty(file) || !dirIsAncestorOf(dir, file) ||
		s.nearestPackage(parentDir(file)) != dir {
		log.Printf("typescript: %s names %s, which is not in %s, so nothing is "+
			"staged as wrangler_config", cfg, literal, orRepoRoot(dir))
		return ""
	}
	st, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(file)))
	if err != nil || st.IsDir() {
		log.Printf("typescript: %s names %s, which is not there, so nothing is "+
			"staged as wrangler_config", cfg, file)
		return ""
	}
	return file
}

// wranglerConfigRule is the filegroup over the wrangler config the exported
// vitest config at cfg names; nil when it names none.
func wranglerConfigRule(args language.GenerateArgs, tc *tsConfig,
	cfg string) *rule.Rule {
	file := tc.programs.wranglerConfigOf(args.Config.RepoRoot, cfg)
	if file == "" {
		return nil
	}
	r := rule.NewRule("filegroup", wranglerConfigTargetName)
	r.SetAttr("srcs", packageSrcs(args, []string{file}))
	r.SetAttr("visibility", []string{"//visibility:public"})
	return r
}

func importsWorkersPool(edges []explainfiles.Edge) bool {
	for _, e := range edges {
		if e.Kind == explainfiles.Import && isBareSpecifier(e.Specifier) &&
			barePackageName(e.Specifier) == workersPoolPackage {
			return true
		}
	}
	return false
}

// workersPoolAttrs writes what a ts_test whose config imports the pool needs;
// the istanbul label it returns is a dep, "" when the lockfile lacks it.
func workersPoolAttrs(c *config.Config, tc *tsConfig, r *rule.Rule, cfg string,
	edges []explainfiles.Edge, from label.Label) string {
	if !importsWorkersPool(edges) {
		return ""
	}
	if file := tc.programs.wranglerConfigOf(c.RepoRoot, cfg); file != "" {
		r.SetAttr("wrangler_config", wranglerConfigLabel(file, cfg, from.Pkg))
	}
	if tc.lock == nil || !tc.lock.names[istanbulPackage] {
		log.Printf("typescript: %s: %s runs the Workers pool, which refuses v8 "+
			"coverage, and %s is not in %s; no coverage_provider, so bazel "+
			"coverage fails on it", from, cfg, istanbulPackage, pnpmLockfileName)
		return ""
	}
	r.SetAttr("coverage_provider", "istanbul")
	return tc.lock.label(istanbulPackage, from.Pkg)
}

// The file by name from the config's own package, else the owner's filegroup.
func wranglerConfigLabel(file, cfg, pkg string) string {
	if dir := parentDir(cfg); dir != pkg {
		return "//" + dir + ":" + wranglerConfigTargetName
	}
	return strings.TrimPrefix(file, pkg+"/")
}
