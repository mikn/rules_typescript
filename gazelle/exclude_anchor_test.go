package typescript

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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

// convergeWorkspace is one gazelle run over a whole little workspace, written
// to a temp root and left on disk for the assertions to read: the boundary mode,
// the framework staging walk and the tsconfig label are all decided by the walk
// rather than by one generateRules call, so generateUnder cannot see them.
func convergeWorkspace(t *testing.T, files map[string]string) (root, logged string) {
	t.Helper()
	root = t.TempDir()
	writeWorkspace(t, root, files)
	logged = captureLog(t, func() { convergeGazelle(t, root) })
	return root, logged
}

// srcsOfKind is every srcs entry of every rule of one kind in one package, read
// back off disk, so a test can say what is in the program.
func srcsOfKind(t *testing.T, root, pkg, kind string) []string {
	t.Helper()
	var out []string
	for _, r := range loadRules(t, root, pkg) {
		if r.Kind() == kind {
			out = append(out, r.AttrStrings("srcs")...)
		}
	}
	sort.Strings(out)
	return out
}

// TestExclude_DirectoryPatternPrunesOnlyARolledUpWalk is the boundary-mode
// truth about naming a directory, which is what the docs and the diagnostic may
// claim and no more.
//
// A directory pattern is read in one place only: the rollup walk, which runs in
// the modes where a plain subdirectory is not a package. Under the default
// every-dir mode the subdirectory is a package in its own right, generation
// there never asks about the directory's own name, and the pattern changes
// nothing. Gazelle's own # gazelle:exclude is what prunes the walk itself.
func TestExclude_DirectoryPatternPrunesOnlyARolledUpWalk(t *testing.T) {
	for _, tt := range []struct {
		mode      string
		subIsGone bool
	}{
		{"", false},
		{"# gazelle:ts_package_boundary index-only\n", true},
		{"# gazelle:ts_package_boundary tsconfig\n", true},
	} {
		name := tt.mode
		if name == "" {
			name = "every-dir (default)"
		}
		// Bare and anchored: naming the directory and naming its path are the
		// same question asked of the same walk.
		for _, pattern := range []string{"sub", "./sub"} {
			t.Run(strings.TrimSpace(name)+" "+pattern, func(t *testing.T) {
				root, _ := convergeWorkspace(t, map[string]string{
					"BUILD.bazel":       tt.mode,
					"web/BUILD.bazel":   "# gazelle:ts_exclude " + pattern + "\n",
					"web/tsconfig.json": `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
					"web/index.ts":      "export const i = 1;\n",
					"web/sub/s.ts":      "export const s = 1;\n",
				})

				web := srcsOfKind(t, root, "web", "ts_compile")
				if hasSrc(web, "sub/s.ts") {
					t.Errorf("web claimed sub/s.ts despite the directory pattern: %v", web)
				}
				sub := srcsOfKind(t, root, "web/sub", "ts_compile")
				if tt.subIsGone && len(sub) > 0 {
					t.Errorf("web/sub still compiles %v under a rolled-up boundary", sub)
				}
				if !tt.subIsGone && !hasSrc(sub, "s.ts") {
					t.Errorf("web/sub lost s.ts under every-dir mode, where a directory "+
						"pattern reaches nothing: %v", sub)
				}
			})
		}
	}
}

// TestExclude_AnchoredPatternWithNoPathIsRefusedOutLoud: "./" resolves to the
// declaring directory's own path, and no call site compares anything against
// that, so the pattern would match nothing at all -- the guard exists to say
// that rather than to prevent a drop.
func TestExclude_AnchoredPatternWithNoPathIsRefusedOutLoud(t *testing.T) {
	var srcs []string
	logged := captureLog(t, func() {
		srcs = compiledSrcs(t, generateUnder(t, map[string]string{
			"web/BUILD.bazel": "# gazelle:ts_exclude ./\n",
			"web/app.ts":      "export const a = 1;\n",
			"web/other.ts":    "export const o = 1;\n",
		}, "web"))
	})

	for _, want := range []string{"ts_exclude ./", "excludes nothing"} {
		if !strings.Contains(logged, want) {
			t.Errorf("a pattern that can match nothing was accepted quietly, no %q:\n%s",
				want, logged)
		}
	}
	for _, kept := range []string{"app.ts", "other.ts"} {
		if !hasSrc(srcs, kept) {
			t.Errorf("./ alone took %s out of the package: %v", kept, srcs)
		}
	}
}

// TestExclude_ReportNamesTheDeclaringBuildFile: directives are inherited, so
// the package a drop fires in is usually not the package holding the line to
// edit. A report that names only the former sends the reader to a build file
// the directive is not in.
func TestExclude_ReportNamesTheDeclaringBuildFile(t *testing.T) {
	logged := captureLog(t, func() {
		generateUnder(t, map[string]string{
			"BUILD.bazel":    "# gazelle:ts_exclude *.gen.ts\n",
			"web/mod.gen.ts": "export const g = 1;\n",
			"web/app.ts":     "export const a = 1;\n",
		}, "web")
	})

	if !strings.Contains(logged, "declared in the workspace root") {
		t.Errorf("the report does not say which build file holds the directive:\n%s", logged)
	}
	if !strings.Contains(logged, "web/mod.gen.ts") {
		t.Errorf("the report does not name the dropped file by its workspace path:\n%s", logged)
	}
}

// adviceRe is the anchored spelling the report offers, quoted, so a test can
// take the message's own advice rather than a hand-copied version of it.
var adviceRe = regexp.MustCompile(`"(\./[^"]+)"`)

// TestExclude_TheAnchoredSpellingTheReportGivesWorks takes the advice out of
// the message and runs it, written where the message says to write it. The two
// previous rounds of this both shipped a fix that did nothing at the
// declaration site; nothing short of executing the printed string catches that.
func TestExclude_TheAnchoredSpellingTheReportGivesWorks(t *testing.T) {
	files := map[string]string{
		"BUILD.bazel":        "# gazelle:ts_exclude *.gen.ts\n",
		"web/mod.gen.ts":     "export const g = 1;\n",
		"web/app.ts":         "export const a = 1;\n",
		"web/sub/mod.gen.ts": "export const g = 1;\n",
	}
	logged := captureLog(t, func() { generateUnder(t, files, "web") })

	found := adviceRe.FindStringSubmatch(logged)
	if found == nil {
		t.Fatalf("the report offers no anchored spelling to follow:\n%s", logged)
	}
	advice := found[1]

	// Written where the message says: the build file that declares the pattern.
	files["BUILD.bazel"] = "# gazelle:ts_exclude " + advice + "\n"
	web := compiledSrcs(t, generateUnder(t, files, "web"))
	if hasSrc(web, "mod.gen.ts") {
		t.Errorf("%q, written in the build file the report names, dropped nothing in web: %v",
			advice, web)
	}
	if !hasSrc(web, "app.ts") {
		t.Errorf("%q took the rest of web with it: %v", advice, web)
	}
	sub := compiledSrcs(t, generateUnder(t, files, "web/sub"))
	if !hasSrc(sub, "mod.gen.ts") {
		t.Errorf("%q was supposed to match web's own files only, and reached web/sub: %v",
			advice, sub)
	}
}

// TestExclude_AnchoredPatternThroughGazelleTsJson: the json surface takes the
// same values a directive does, and an anchored entry resolves against the
// directory holding the gazelle_ts.json that carries it.
func TestExclude_AnchoredPatternThroughGazelleTsJson(t *testing.T) {
	files := map[string]string{
		"web/gazelle_ts.json":    `{"excludePatterns": ["./vite.config.ts"]}` + "\n",
		"web/vite.config.ts":     "export default {};\n",
		"web/app.ts":             "export const a = 1;\n",
		"web/sub/vite.config.ts": "export default {};\n",
	}

	declaring := compiledSrcs(t, generateUnder(t, files, "web"))
	if hasSrc(declaring, "vite.config.ts") {
		t.Errorf("an anchored json entry dropped nothing in web: %v", declaring)
	}
	if !hasSrc(declaring, "app.ts") {
		t.Errorf("an anchored json entry took the rest of web: %v", declaring)
	}
	nested := compiledSrcs(t, generateUnder(t, files, "web/sub"))
	if !hasSrc(nested, "vite.config.ts") {
		t.Errorf("an anchored json entry reached the namesake below it: %v", nested)
	}
}

// stagingSrcsOf is the staging_srcs of the generated bundle, which is the list
// of labels the framework walk decided to name.
func stagingSrcsOf(t *testing.T, root string) []string {
	t.Helper()
	for _, r := range loadRules(t, root, "") {
		if r.Kind() == "ts_bundle" {
			return r.AttrStrings("staging_srcs")
		}
	}
	t.Fatalf("no ts_bundle generated:\n%s", buildFileText(t, root, ""))
	return nil
}

// TestExclude_AnchoredDirectoryPatternSkipsAStagedSubtree pins
// stagingLabelsOutside: the walk that decides which "sources" filegroups the
// bundle names reads the exclusion workspace-relative, since it runs only at
// the root. A namesake directory elsewhere is what says the anchor held.
func TestExclude_AnchoredDirectoryPatternSkipsAStagedSubtree(t *testing.T) {
	root, _ := convergeWorkspace(t, map[string]string{
		"BUILD.bazel":               "# gazelle:ts_exclude ./shared/vendor\n",
		"package.json":              tanstackPackageJSON,
		"src/app/main.tsx":          "export const m = 1;\n",
		"src/routes/index.tsx":      "export const i = 1;\n",
		"shared/hub.ts":             "export const h = 1;\n",
		"shared/vendor/only.ts":     "export const o = 1;\n",
		"shared/vendor/deep/two.ts": "export const t = 1;\n",
		"shared/other/vendor/x.ts":  "export const x = 1;\n",
	})

	staging := stagingSrcsOf(t, root)
	for _, absent := range []string{"//shared/vendor", "//shared/vendor/deep"} {
		if slices.Contains(staging, absent) {
			t.Errorf("staging_srcs = %v still names %q, which the exclusion skipped",
				staging, absent)
		}
	}
	if !slices.Contains(staging, "//shared/other/vendor") {
		t.Errorf("staging_srcs = %v lost the namesake directory the anchor does not name",
			staging)
	}
}

// TestExclude_AnchoredFilePatternDropsOneStagedPackagesLabel pins
// packageStagingLabels, the other half of the staging walk: excluding the only
// source in a directory withdraws that directory's label and nothing below it,
// where excluding the directory itself withdraws the subtree.
func TestExclude_AnchoredFilePatternDropsOneStagedPackagesLabel(t *testing.T) {
	root, _ := convergeWorkspace(t, map[string]string{
		"BUILD.bazel":               "# gazelle:ts_exclude ./shared/vendor/only.ts\n",
		"package.json":              tanstackPackageJSON,
		"src/app/main.tsx":          "export const m = 1;\n",
		"src/routes/index.tsx":      "export const i = 1;\n",
		"shared/vendor/only.ts":     "export const o = 1;\n",
		"shared/vendor/deep/two.ts": "export const t = 1;\n",
	})

	staging := stagingSrcsOf(t, root)
	if slices.Contains(staging, "//shared/vendor") {
		t.Errorf("staging_srcs = %v names //shared/vendor, whose only source was excluded, so "+
			"generation writes no target there", staging)
	}
	if !slices.Contains(staging, "//shared/vendor/deep") {
		t.Errorf("staging_srcs = %v lost the subtree below the excluded file", staging)
	}
}

// TestExclude_AnchoredPatternHidesTheFrameworkEntry pins entryTargetIsCovered:
// the entry_point label is decided by reading the entry package off disk, and an
// anchored pattern naming the entry has to be read there too. Otherwise the
// bundle names an entry_point nothing writes, and a dangling label fails
// analysis for every target that reaches it.
func TestExclude_AnchoredPatternHidesTheFrameworkEntry(t *testing.T) {
	root, logged := convergeWorkspace(t, map[string]string{
		"BUILD.bazel":          "# gazelle:ts_exclude ./app/entry.client.tsx\n",
		"package.json":         remixPackageJSON,
		"app/entry.client.tsx": remixEntryClient,
		"app/root.tsx":         remixRoot,
	})

	if !strings.Contains(logged, "declares the client entry target") {
		t.Errorf("the withdrawn entry was not reported:\n%s", logged)
	}
	for _, r := range loadRules(t, root, "") {
		if r.Kind() == "ts_bundle" {
			t.Errorf("a ts_bundle was written with entry_point = %q, which the exclusion left "+
				"nothing to name", r.AttrString("entry_point"))
		}
	}
}

// TestExclude_AnchoredPatternDropsTheTsConfigTarget pins the two tsconfig call
// sites at once. ownTsConfigRule decides whether the directory holding the file
// writes a ts_config, and tsConfigLabel decides whether a package below it may
// name one -- asked about the directory holding the file, which is where an
// anchored pattern naming it was resolved.
func TestExclude_AnchoredPatternDropsTheTsConfigTarget(t *testing.T) {
	root, _ := convergeWorkspace(t, map[string]string{
		"BUILD.bazel":         "# gazelle:ts_exclude ./web/tsconfig.json\n",
		"web/tsconfig.json":   `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"web/src/a.ts":        "export const a = 1;\n",
		"other/tsconfig.json": `{"compilerOptions":{"lib":["es2022"]}}` + "\n",
		"other/src/b.ts":      "export const b = 1;\n",
	})

	if _, ok := attrOf(t, root, "web", "ts_config", tsConfigTargetName, "src"); ok {
		t.Error("web wrote a ts_config for a tsconfig.json the exclusion names")
	}
	if got, _ := attrOf(t, root, "web/src", "ts_compile", "src", "tsconfig"); got != "" {
		t.Errorf("web/src names tsconfig = %q, a target the exclusion stopped anyone writing",
			got)
	}
	if _, ok := attrOf(t, root, "other", "ts_config", tsConfigTargetName, "src"); !ok {
		t.Error("the namesake tsconfig.json the anchor does not name lost its ts_config too")
	}
}

// TestExclude_AKeptSrcsEntryGoesOnCompiling is why the report claims no more
// than this run's srcs: exclusion happens at generation time and never sees the
// merge, and rule.MergeList keeps a list element carrying "# keep".
func TestExclude_AKeptSrcsEntryGoesOnCompiling(t *testing.T) {
	root, logged := convergeWorkspace(t, map[string]string{
		"BUILD.bazel": "",
		"web/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

# gazelle:ts_exclude vite.config.ts

ts_compile(
    name = "web",
    srcs = [
        "app.ts",
        "vite.config.ts",  # keep
    ],
    visibility = ["//visibility:public"],
)
`,
		"web/app.ts":         "export const a = 1;\n",
		"web/vite.config.ts": "export default {};\n",
	})

	if !hasSrc(srcsOfKind(t, root, "web", "ts_compile"), "vite.config.ts") {
		t.Fatal("a \"# keep\" srcs entry did not survive the merge, so the report may claim " +
			"more than this run's srcs")
	}
	if strings.Contains(logged, "nothing in the build compiles") {
		t.Errorf("the report claims the file reaches no compile, and a kept srcs entry "+
			"compiles it:\n%s", logged)
	}
}
