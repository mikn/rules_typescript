package typescript

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/merger"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

// remixWorkspace is the smallest tree that makes the Remix framework path fire:
// the @remix-run/* deps drive detection, app/ holds the entry and the routes.
var remixWorkspace = map[string]string{
	"package.json": `{
  "name": "w",
  "dependencies": {
    "@remix-run/dev": "2.17.4",
    "@remix-run/node": "2.17.4",
    "@remix-run/react": "2.17.4",
    "react": "19.1.0",
    "react-dom": "19.1.0"
  }
}
`,
	"index.html":                     "<html></html>\n",
	"app/entry.client.tsx":           "export {};\n",
	"app/root.tsx":                   "export default function Root() { return null; }\n",
	"app/routes/_index.tsx":          "export default function Index() { return null; }\n",
	"app/routes/panel/route.tsx":     "export default function Panel() { return null; }\n",
	"app/routes/panel/helper.ts":     "export const helper = 1;\n",
	"app/routes/panel/panel.test.ts": "export const t = 1;\n",
}

func writeWorkspace(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// gazellePass does what one `gazelle` invocation does: walk every directory,
// generate rules, merge them into the BUILD file that is already there and
// write it back. Calling generateRules once cannot observe a rule that is
// written on the first run and then never revisited, so the merge and the
// re-read have to be part of the test.
func gazellePass(t *testing.T, repoRoot string) {
	t.Helper()
	lang := &tsLang{}
	kinds := lang.Kinds()

	for _, rel := range walkDirs(t, repoRoot) {
		dir := filepath.Join(repoRoot, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		buildPath := filepath.Join(dir, "BUILD.bazel")
		var regular []string
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == "BUILD.bazel" {
				continue
			}
			regular = append(regular, entry.Name())
		}
		sort.Strings(regular)

		f := loadBuildFile(t, buildPath, rel)
		res := generateRules(language.GenerateArgs{
			Config:       configFor(repoRoot, rel),
			Dir:          dir,
			Rel:          rel,
			File:         f,
			RegularFiles: regular,
		})
		merger.MergeFile(f, res.Empty, res.Gen, merger.PreResolve, kinds, nil)
		merger.FixLoads(f, lang.Loads())
		f.Sync()
		if err := f.Save(buildPath); err != nil {
			t.Fatal(err)
		}
	}
}

// configFor rebuilds the directive/framework config chain from the root down to
// rel, the way Gazelle's per-directory config inheritance does.
func configFor(repoRoot, rel string) *config.Config {
	c := &config.Config{RepoRoot: repoRoot, Exts: make(map[string]interface{})}
	configureTsConfig(c, "", nil)
	if rel == "" {
		return c
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		configureTsConfig(c, strings.Join(parts[:i+1], "/"), nil)
	}
	return c
}

func loadBuildFile(t *testing.T, path, rel string) *rule.File {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		return rule.EmptyFile(path, rel)
	}
	f, err := rule.LoadFile(path, rel)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// walkDirs returns every directory in repoRoot as a slash path, root first.
func walkDirs(t *testing.T, repoRoot string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(repoRoot, func(p string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(dirs)
	return dirs
}

// filegroupSrcs returns the srcs of the "sources" filegroup in a generated
// BUILD file, and whether the rule is there at all.
func filegroupSrcs(t *testing.T, buildPath string) ([]string, bool) {
	t.Helper()
	f, err := rule.LoadFile(buildPath, "")
	if err != nil {
		t.Fatalf("cannot read %s: %v", buildPath, err)
	}
	for _, r := range f.Rules {
		if r.Kind() == "filegroup" && r.Name() == "sources" {
			return r.AttrStrings("srcs"), true
		}
	}
	return nil, false
}

func attrStrings(t *testing.T, buildPath, kind, name, attr string) []string {
	t.Helper()
	f, err := rule.LoadFile(buildPath, "")
	if err != nil {
		t.Fatalf("cannot read %s: %v", buildPath, err)
	}
	for _, r := range f.Rules {
		if r.Kind() == kind && r.Name() == name {
			return r.AttrStrings(attr)
		}
	}
	t.Fatalf("%s has no %s(name = %q)", buildPath, kind, name)
	return nil
}

// TestRemixFolderRoute_StagedSourcesFollowTheDirectory runs the generator twice
// with a colocated module added in between. The first run is the one that used
// to be right: the second was where ts_compile picked the new file up and the
// staged filegroup did not, so the staging tree the bundle reads and the tree
// the compiler sees drifted apart for good.
func TestRemixFolderRoute_StagedSourcesFollowTheDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)

	gazellePass(t, repoRoot)
	panelBuild := filepath.Join(repoRoot, "app", "routes", "panel", "BUILD.bazel")
	srcs, ok := filegroupSrcs(t, panelBuild)
	if !ok {
		t.Fatalf("first run wrote no sources filegroup in %s", panelBuild)
	}
	if strings.Join(srcs, ",") != "helper.ts,route.tsx" {
		t.Fatalf("after run 1 filegroup srcs = %v, want [helper.ts route.tsx]", srcs)
	}

	writeWorkspace(t, repoRoot, map[string]string{
		"app/routes/panel/extra.ts": "export const extra = 2;\n",
	})
	gazellePass(t, repoRoot)

	want := "extra.ts,helper.ts,route.tsx"
	srcs, ok = filegroupSrcs(t, panelBuild)
	if !ok {
		t.Fatalf("second run dropped the sources filegroup from %s", panelBuild)
	}
	if got := strings.Join(srcs, ","); got != want {
		t.Errorf("after run 2 filegroup srcs = %v, want [%s]: the staged tree no longer matches the directory", srcs, want)
	}
	if got := strings.Join(attrStrings(t, panelBuild, "ts_compile", "panel", "srcs"), ","); got != want {
		t.Errorf("after run 2 ts_compile srcs = %s, want %s", got, want)
	}

	// And back the other way: a removed file has to leave the staging tree too,
	// or the bundle stages a label naming a file that is not there.
	if err := os.Remove(filepath.Join(repoRoot, "app", "routes", "panel", "helper.ts")); err != nil {
		t.Fatal(err)
	}
	gazellePass(t, repoRoot)
	srcs, _ = filegroupSrcs(t, panelBuild)
	if got := strings.Join(srcs, ","); got != "extra.ts,route.tsx" {
		t.Errorf("after the delete filegroup srcs = %v, want [extra.ts route.tsx]", srcs)
	}
}

// TestRemixFolderRoute_StagedSourcesSkipTests pins what the staged filegroup is
// allowed to name: the bundle stages sources, and a test module is not one.
func TestRemixFolderRoute_StagedSourcesSkipTests(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)
	gazellePass(t, repoRoot)

	srcs, _ := filegroupSrcs(t, filepath.Join(repoRoot, "app", "routes", "panel", "BUILD.bazel"))
	for _, src := range srcs {
		if strings.Contains(src, ".test.") {
			t.Errorf("filegroup srcs = %v, which stages a test module", srcs)
		}
	}
}

// TestRemixFolderRoute_StagingSrcsNamesEveryEmittedFilegroup: the root bundle's
// staging_srcs and the filegroups in the route directories are two halves of one
// fact. A label with no filegroup is a build error; a filegroup with no label is
// a route that is absent from the bundle and breaks nothing.
func TestRemixFolderRoute_StagingSrcsNamesEveryEmittedFilegroup(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)
	writeWorkspace(t, repoRoot, map[string]string{
		"app/routes/panel/nested/thing.ts": "export const thing = 3;\n",
	})
	gazellePass(t, repoRoot)

	staged := map[string]bool{}
	for _, label := range attrStrings(t, filepath.Join(repoRoot, "BUILD.bazel"), "ts_bundle", "app_remix", "staging_srcs") {
		staged[label] = true
	}

	for _, dir := range []string{"app/routes/panel", "app/routes/panel/nested"} {
		buildPath := filepath.Join(repoRoot, filepath.FromSlash(dir), "BUILD.bazel")
		if _, ok := filegroupSrcs(t, buildPath); !ok {
			t.Errorf("%s has no sources filegroup", dir)
			continue
		}
		if label := "//" + dir + ":sources"; !staged[label] {
			t.Errorf("%s exports a sources filegroup that staging_srcs does not name", label)
		}
	}
}

// TestRemixFolderRoute_AddedAfterTheFirstRunReachesTheBundle is the other half
// of the drift: not a new file in a staged directory but a whole new folder
// route. The bundle rule is generated only on the run that creates it, so from
// the second run on the staging_srcs to edit is the one in the BUILD file.
func TestRemixFolderRoute_AddedAfterTheFirstRunReachesTheBundle(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)
	gazellePass(t, repoRoot)

	writeWorkspace(t, repoRoot, map[string]string{
		"app/routes/later/route.tsx": "export default function Later() { return null; }\n",
		"app/routes/later/bit.ts":    "export const bit = 1;\n",
	})
	gazellePass(t, repoRoot)

	label := "//app/routes/later:sources"
	if _, ok := filegroupSrcs(t, filepath.Join(repoRoot, "app", "routes", "later", "BUILD.bazel")); !ok {
		t.Fatalf("the new folder route got no sources filegroup")
	}
	staged := attrStrings(t, filepath.Join(repoRoot, "BUILD.bazel"), "ts_bundle", "app_remix", "staging_srcs")
	for _, entry := range staged {
		if entry == label {
			return
		}
	}
	t.Errorf("staging_srcs = %v, missing %s: the route has a filegroup nothing stages, so it is absent from the bundle and the build stays green", staged, label)
}

// TestRemixFolderRoute_DeletedRouteLeavesStagingSrcs: the label has to go when
// the directory does, or every later build fails on a package that is not there
// and re-running Gazelle never clears it.
func TestRemixFolderRoute_DeletedRouteLeavesStagingSrcs(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)
	gazellePass(t, repoRoot)
	if err := os.RemoveAll(filepath.Join(repoRoot, "app", "routes", "panel")); err != nil {
		t.Fatal(err)
	}
	gazellePass(t, repoRoot)

	staged := attrStrings(t, filepath.Join(repoRoot, "BUILD.bazel"), "ts_bundle", "app_remix", "staging_srcs")
	for _, entry := range staged {
		if entry == "//app/routes/panel:sources" {
			t.Errorf("staging_srcs = %v still names the deleted folder route", staged)
		}
	}
	for _, entry := range []string{"index.html", "package.json", "//app:sources", "//app/routes:sources"} {
		if !contains(staged, entry) {
			t.Errorf("staging_srcs = %v dropped %s", staged, entry)
		}
	}
}

// TestRemixFolderRoute_StagedEvenWhenAHandWrittenTargetCompilesTheFiles: the
// bundle reads the route from the staging tree, so the filegroup follows the
// directory rather than the generated ts_compile. A hand-written target claiming
// every file leaves no generated ts_compile to read srcs from, and the label in
// staging_srcs would then name nothing.
func TestRemixFolderRoute_StagedEvenWhenAHandWrittenTargetCompilesTheFiles(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)
	writeWorkspace(t, repoRoot, map[string]string{
		"app/routes/panel/BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_compile")

ts_compile(
    name = "handwritten",
    srcs = [
        "helper.ts",
        "route.tsx",
    ],
)
`,
	})
	gazellePass(t, repoRoot)

	panelBuild := filepath.Join(repoRoot, "app", "routes", "panel", "BUILD.bazel")
	srcs, ok := filegroupSrcs(t, panelBuild)
	if !ok {
		t.Fatalf("no sources filegroup in %s, so //app/routes/panel:sources names nothing", panelBuild)
	}
	if got := strings.Join(srcs, ","); got != "helper.ts,route.tsx" {
		t.Errorf("filegroup srcs = %s, want helper.ts,route.tsx", got)
	}
}

func contains(haystack []string, needle string) bool {
	for _, entry := range haystack {
		if entry == needle {
			return true
		}
	}
	return false
}

// TestRemixFolderRoute_KeepOnStagingSrcsIsHonoured: editing the rule in the
// BUILD file goes around the merger, so the plugin has to honour "# keep"
// itself or a hand-maintained staging_srcs is rewritten anyway.
func TestRemixFolderRoute_KeepOnStagingSrcsIsHonoured(t *testing.T) {
	repoRoot := t.TempDir()
	writeWorkspace(t, repoRoot, remixWorkspace)
	writeWorkspace(t, repoRoot, map[string]string{
		"BUILD.bazel": `load("@rules_typescript//ts:defs.bzl", "ts_bundle")

ts_bundle(
    name = "app_remix",
    bundler = ":vite",
    entry_point = "//app:entry_client",
    html = "index.html",
    mode = "app",
    # keep
    staging_srcs = [
        "index.html",
        "package.json",
        "//app:sources",
    ],
    vite_config = "remix-vite.config.mjs",
)
`,
	})
	gazellePass(t, repoRoot)

	staged := attrStrings(t, filepath.Join(repoRoot, "BUILD.bazel"), "ts_bundle", "app_remix", "staging_srcs")
	if got := strings.Join(staged, ","); got != "index.html,package.json,//app:sources" {
		t.Errorf("staging_srcs = %s, want the kept list unchanged", got)
	}
}
