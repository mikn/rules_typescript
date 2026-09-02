package typescript

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// generateUnder writes a whole little workspace and generates rules for one
// package in it, walking the directive chain from the root down the way Gazelle
// does. runGenerateWithBuild configures two directories only, and a directive
// declared in web/ read by a run in web/sub is exactly what anchoring is about.
//
// A files key ending in "/" is a directory to create and nothing else; a key
// with a slash in it is written but left out of the generated package's
// RegularFiles, which is where Gazelle puts a file of another directory.
func generateUnder(t *testing.T, files map[string]string, pkg string) language.GenerateResult {
	t.Helper()

	repoRoot := t.TempDir()
	for name, content := range files {
		full := filepath.Join(repoRoot, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	var pkgFile *rule.File
	for _, rel := range relChain(pkg) {
		f := buildFileIn(t, repoRoot, rel)
		configureTsConfig(c, rel, f)
		if rel == pkg {
			pkgFile = f
		}
	}

	dir := filepath.Join(repoRoot, filepath.FromSlash(pkg))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "BUILD.bazel" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	return generateRules(language.GenerateArgs{
		Config:       c,
		Dir:          dir,
		Rel:          pkg,
		File:         pkgFile,
		RegularFiles: names,
	})
}

// relChain is every directory from the workspace root down to rel, inclusive,
// which is the sequence of Configure calls Gazelle's walk makes.
func relChain(rel string) []string {
	chain := []string{""}
	if rel == "" {
		return chain
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		chain = append(chain, path.Join(parts[:i+1]...))
	}
	return chain
}

// buildFileIn is the directory's BUILD file, or nil when it has none: Gazelle
// calls Configure with a nil file for a directory without one.
func buildFileIn(t *testing.T, repoRoot, rel string) *rule.File {
	t.Helper()
	f, err := rule.LoadFile(filepath.Join(repoRoot, filepath.FromSlash(rel), "BUILD.bazel"), rel)
	if err != nil {
		return nil
	}
	return f
}

// compiledSrcs is the srcs of the ts_compile generated for pkg, so a test can
// say what is in the program rather than what a rule happens to be named.
func compiledSrcs(t *testing.T, res language.GenerateResult) []string {
	t.Helper()
	var out []string
	for _, r := range res.Gen {
		if r.Kind() == "ts_compile" {
			out = append(out, r.AttrStrings("srcs")...)
		}
	}
	sort.Strings(out)
	return out
}

func hasSrc(srcs []string, want string) bool {
	for _, s := range srcs {
		if s == want {
			return true
		}
	}
	return false
}

// TestExclude_BarePatternDropsTheBasenameAtEveryDepth pins the behaviour people
// already rely on: a pattern with no path in it matches the basename wherever
// it turns up in the tree, including a package below the one that declared it.
func TestExclude_BarePatternDropsTheBasenameAtEveryDepth(t *testing.T) {
	files := map[string]string{
		"web/BUILD.bazel":        "# gazelle:ts_exclude vite.config.ts\n",
		"web/vite.config.ts":     "export default {};\n",
		"web/app.ts":             "export const a = 1;\n",
		"web/sub/vite.config.ts": "export default {};\n",
		"web/sub/thing.ts":       "export const t = 1;\n",
	}

	root := compiledSrcs(t, generateUnder(t, files, "web"))
	if hasSrc(root, "vite.config.ts") {
		t.Errorf("web: the declaring package's own file survived a bare pattern: %v", root)
	}
	sub := compiledSrcs(t, generateUnder(t, files, "web/sub"))
	if hasSrc(sub, "vite.config.ts") {
		t.Errorf("web/sub: a bare pattern must still reach the nested namesake: %v", sub)
	}
}

// TestExclude_AnchoredPatternDropsThatOnePath: the "./" form anchors to the
// directory that declared it, and a package-root file is what it is for.
func TestExclude_AnchoredPatternDropsThatOnePath(t *testing.T) {
	res := generateUnder(t, map[string]string{
		"web/BUILD.bazel":    "# gazelle:ts_exclude ./vite.config.ts\n",
		"web/vite.config.ts": "export default {};\n",
		"web/app.ts":         "export const a = 1;\n",
	}, "web")

	srcs := compiledSrcs(t, res)
	if hasSrc(srcs, "vite.config.ts") {
		t.Errorf("./vite.config.ts did not drop web/vite.config.ts: %v", srcs)
	}
	if !hasSrc(srcs, "app.ts") {
		t.Errorf("the rest of the package went missing: %v", srcs)
	}
}

// TestExclude_AnchoredPatternSparesTheNestedNamesake is the whole point: the
// silent second drop a bare pattern makes is what the anchored form refuses.
//
// Both halves, because "the namesake survived" is also what a pattern matching
// nothing at all looks like: the drop in web/ is what says the pattern works.
func TestExclude_AnchoredPatternSparesTheNestedNamesake(t *testing.T) {
	files := map[string]string{
		"web/BUILD.bazel":        "# gazelle:ts_exclude ./vite.config.ts\n",
		"web/vite.config.ts":     "export default {};\n",
		"web/sub/vite.config.ts": "export default {};\n",
	}

	declaring := compiledSrcs(t, generateUnder(t, files, "web"))
	if hasSrc(declaring, "vite.config.ts") {
		t.Errorf("web: the anchored path was not dropped at all: %v", declaring)
	}
	nested := compiledSrcs(t, generateUnder(t, files, "web/sub"))
	if !hasSrc(nested, "vite.config.ts") {
		t.Errorf("web/sub: an anchored pattern reached a namesake one directory down: %v", nested)
	}
}

// TestExclude_AnchoredPatternReachesIntoASubdirectory: "./" resolves against the
// declaring directory at any depth, so one directive can name a file below it.
func TestExclude_AnchoredPatternReachesIntoASubdirectory(t *testing.T) {
	res := generateUnder(t, map[string]string{
		"web/BUILD.bazel":    "# gazelle:ts_exclude ./plugins/one.ts\n",
		"web/app.ts":         "export const a = 1;\n",
		"web/plugins/one.ts": "export const one = 1;\n",
		"web/plugins/two.ts": "export const two = 2;\n",
	}, "web/plugins")

	srcs := compiledSrcs(t, res)
	if hasSrc(srcs, "one.ts") {
		t.Errorf("./plugins/one.ts did not drop web/plugins/one.ts: %v", srcs)
	}
	if !hasSrc(srcs, "two.ts") {
		t.Errorf("./plugins/one.ts dropped its sibling too: %v", srcs)
	}
}

// TestExclude_AnchoredPatternInTheRollupWalk: under a rolled-up boundary the
// files of a subdirectory are the package's own srcs, reached by a joined path,
// and anchoring has to mean the same thing there.
func TestExclude_AnchoredPatternInTheRollupWalk(t *testing.T) {
	res := generateUnder(t, map[string]string{
		"BUILD.bazel":     "# gazelle:ts_package_boundary index-only\n",
		"web/BUILD.bazel": "# gazelle:ts_exclude ./x.ts\n",
		"web/index.ts":    "export const i = 1;\n",
		"web/x.ts":        "export const x = 1;\n",
		"web/sub/x.ts":    "export const sx = 1;\n",
		"web/sub/y.ts":    "export const sy = 1;\n",
	}, "web")

	srcs := compiledSrcs(t, res)
	if hasSrc(srcs, "x.ts") {
		t.Errorf("./x.ts did not drop the package's own x.ts: %v", srcs)
	}
	if !hasSrc(srcs, "sub/x.ts") {
		t.Errorf("./x.ts reached the rolled-up namesake sub/x.ts: %v", srcs)
	}
	if !hasSrc(srcs, "sub/y.ts") {
		t.Errorf("the rest of the rolled-up subtree went missing: %v", srcs)
	}
}

// TestExclude_RollupBarePatternStillDropsEveryDepth: the compat promise again,
// under a rolled-up boundary, where the drop is by joined path.
func TestExclude_RollupBarePatternStillDropsEveryDepth(t *testing.T) {
	res := generateUnder(t, map[string]string{
		"BUILD.bazel":     "# gazelle:ts_package_boundary index-only\n",
		"web/BUILD.bazel": "# gazelle:ts_exclude x.ts\n",
		"web/index.ts":    "export const i = 1;\n",
		"web/x.ts":        "export const x = 1;\n",
		"web/sub/x.ts":    "export const sx = 1;\n",
		"web/sub/y.ts":    "export const sy = 1;\n",
	}, "web")

	srcs := compiledSrcs(t, res)
	for _, gone := range []string{"x.ts", "sub/x.ts"} {
		if hasSrc(srcs, gone) {
			t.Errorf("a bare pattern stopped dropping %s: %v", gone, srcs)
		}
	}
	if !hasSrc(srcs, "sub/y.ts") {
		t.Errorf("the rest of the rolled-up subtree went missing: %v", srcs)
	}
}

// TestExclude_RollupBarePathPatternIsRelativeToThePackage: the other bare
// spelling the docs promise -- a pattern with a "/" in it, matched against the
// path the rollup walk reached a file by, relative to the claiming package.
func TestExclude_RollupBarePathPatternIsRelativeToThePackage(t *testing.T) {
	res := generateUnder(t, map[string]string{
		"BUILD.bazel":       "# gazelle:ts_package_boundary index-only\n",
		"web/BUILD.bazel":   "# gazelle:ts_exclude sub/*.ts\n",
		"web/index.ts":      "export const i = 1;\n",
		"web/sub/x.ts":      "export const sx = 1;\n",
		"web/sub/deep/y.ts": "export const dy = 1;\n",
	}, "web")

	srcs := compiledSrcs(t, res)
	if hasSrc(srcs, "sub/x.ts") {
		t.Errorf("sub/*.ts did not drop sub/x.ts: %v", srcs)
	}
	if !hasSrc(srcs, "sub/deep/y.ts") {
		t.Errorf("sub/*.ts crossed a directory boundary: %v", srcs)
	}
}

// TestExclude_AnchoredGlobStaysInItsDirectory: the report tells a bare-glob
// author that "./*.gen.ts" anchors the pattern to one directory, so that has to
// be what it does -- filepath.Match's "*" not crossing a "/" is what makes it so.
func TestExclude_AnchoredGlobStaysInItsDirectory(t *testing.T) {
	files := map[string]string{
		"web/BUILD.bazel":    "# gazelle:ts_exclude ./*.gen.ts\n",
		"web/mod.gen.ts":     "export const g = 1;\n",
		"web/app.ts":         "export const a = 1;\n",
		"web/sub/mod.gen.ts": "export const g = 1;\n",
	}

	declaring := compiledSrcs(t, generateUnder(t, files, "web"))
	if hasSrc(declaring, "mod.gen.ts") {
		t.Errorf("web: the anchored glob dropped nothing: %v", declaring)
	}
	nested := compiledSrcs(t, generateUnder(t, files, "web/sub"))
	if !hasSrc(nested, "mod.gen.ts") {
		t.Errorf("web/sub: an anchored glob reached a subdirectory: %v", nested)
	}
}

// TestExclude_DropIsReported: a file taken out of the program is said out loud,
// naming the pattern that took it, or the mechanism that exists to control
// silent drops makes them itself.
func TestExclude_DropIsReported(t *testing.T) {
	var logged string
	files := map[string]string{
		"web/BUILD.bazel":    "# gazelle:ts_exclude vite.config.ts\n",
		"web/vite.config.ts": "export default {};\n",
		"web/app.ts":         "export const a = 1;\n",
	}
	logged = captureLog(t, func() { generateUnder(t, files, "web") })

	for _, want := range []string{"ts_exclude", "vite.config.ts", "web"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the report does not mention %q:\n%s", want, logged)
		}
	}
}

// TestExclude_RollupDropIsReported: the rollup path collected no excluded src
// at all, so a pattern over-matching below a rolled-up boundary was the one
// drop nothing could see.
func TestExclude_RollupDropIsReported(t *testing.T) {
	logged := captureLog(t, func() {
		generateUnder(t, map[string]string{
			"BUILD.bazel":     "# gazelle:ts_package_boundary index-only\n",
			"web/BUILD.bazel": "# gazelle:ts_exclude x.ts\n",
			"web/index.ts":    "export const i = 1;\n",
			"web/sub/x.ts":    "export const sx = 1;\n",
		}, "web")
	})

	if !strings.Contains(logged, "sub/x.ts") {
		t.Errorf("a rolled-up file was dropped with no diagnostic:\n%s", logged)
	}
}

// TestExclude_ReportIsOnePerPatternNotOnePerFile: the report has to stay
// readable on the run that does what the directive was written for.
func TestExclude_ReportIsOnePerPatternNotOnePerFile(t *testing.T) {
	files := map[string]string{
		"web/BUILD.bazel": "# gazelle:ts_exclude *.gen.ts\n",
		"web/app.ts":      "export const a = 1;\n",
	}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("web/mod%02d.gen.ts", i)] = "export const g = 1;\n"
	}

	logged := captureLog(t, func() { generateUnder(t, files, "web") })

	var lines int
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		if strings.Contains(line, "ts_exclude") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("40 files under one pattern printed %d lines, want 1:\n%s", lines, logged)
	}
	if !strings.Contains(logged, "40") {
		t.Errorf("the report does not say how many were dropped:\n%s", logged)
	}
}

// TestExclude_PatternThatDropsNothingIsSilent: a directive declared at the root
// for one subtree is in scope everywhere below it, and a report in every
// package it did not match is the noise that gets a diagnostic ignored.
func TestExclude_PatternThatDropsNothingIsSilent(t *testing.T) {
	logged := captureLog(t, func() {
		generateUnder(t, map[string]string{
			"BUILD.bazel": "# gazelle:ts_exclude *.generated.ts\n",
			"web/app.ts":  "export const a = 1;\n",
		}, "web")
	})

	if strings.Contains(logged, "ts_exclude") {
		t.Errorf("a pattern that dropped nothing here still reported:\n%s", logged)
	}
}
