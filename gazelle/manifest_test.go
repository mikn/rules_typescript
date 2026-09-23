package typescript

import (
	"github.com/bazelbuild/bazel-gazelle/rule"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestManifest_Read(t *testing.T) {
	root, _ := npmRepo(t)
	writeFile(t, filepath.Join(root, "workers/download/package.json"), `{
		"name": "download",
		"dependencies": {"zod": "^3.0.0"},
		"devDependencies": {"wrangler": "4.118.0", "typescript": "5.9.2",
			"zod": "^3.0.0"}
	}`)
	m := readManifest(root, "workers/download")
	if m == nil || m.name != "download" || m.dir != "workers/download" {
		t.Fatalf("readManifest = %+v", m)
	}
	if want := []string{"typescript", "wrangler", "zod"}; !slices.Equal(m.deps,
		want) {
		t.Errorf("deps = %v, want %v", m.deps, want)
	}
	if m := readManifest(root, "workers/download/test"); m != nil {
		t.Errorf("a directory without package.json read %+v", m)
	}
	writeFile(t, filepath.Join(root, "broken/package.json"), `{"name": `)
	if m := readManifest(root, "broken"); m != nil {
		t.Errorf("a malformed package.json read %+v", m)
	}
}

// The manifest a package reads is the nearest one at or above its directory.
func TestManifest_Nearest(t *testing.T) {
	root, _ := npmRepo(t)
	for dir, want := range map[string]string{
		"workers/download/test": "workers/download",
		"workers/download":      "workers/download",
		"packages/lib/src/wire": "packages/lib",
		"packages/lib/example":  "packages/lib/example",
		"scripts":               "",
	} {
		m := nearestManifest(root, dir)
		if m == nil || m.dir != want {
			t.Errorf("nearestManifest(%q) = %+v, want dir %q", dir, m, want)
		}
	}
	if m := nearestManifest(t.TempDir(), "a/b"); m != nil {
		t.Errorf("a tree without any package.json read %+v", m)
	}
}

// A ts_test's runtime deps carry the manifest's dependencies and
// devDependencies, lockfile-gated, in D7 spelling; a linked member is its view.
func TestManifestLabels_Union(t *testing.T) {
	root, l := npmRepo(t)
	writeFile(t, filepath.Join(root, "workers/download/package.json"), `{
		"name": "download",
		"dependencies": {"@acme/lib": "workspace:*", "left-pad": "1.3.0"},
		"devDependencies": {"wrangler": "4.118.0", "typescript": "5.9.2"}
	}`)
	var got []string
	out := captureLog(t, func() {
		got = l.manifestLabels(nearestManifest(root, "workers/download/test"),
			"workers/download/test")
	})
	want := []string{
		"//:node_modules/@acme/lib",
		"@npm//workers/download:typescript",
		"@npm//workers/download:wrangler",
	}
	if !slices.Equal(got, want) {
		t.Errorf("manifestLabels = %v, want %v", got, want)
	}
	if !strings.Contains(out, "workers/download/package.json") ||
		!strings.Contains(out, "left-pad") {
		t.Errorf("log %q does not name the manifest and left-pad", out)
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Errorf("%d log lines, want 1: %q", n, out)
	}
}

// The root's manifest names take the flat label; a package under no manifest
// has no union.
func TestManifestLabels_RootAndNone(t *testing.T) {
	root, l := npmRepo(t)
	writeFile(t, filepath.Join(root, "package.json"), `{
		"name": "monorepo",
		"devDependencies": {"typescript": "5.9.2", "vite": "8.2.2"}
	}`)
	got := l.manifestLabels(nearestManifest(root, "scripts"), "scripts")
	if want := []string{"@npm//:typescript", "@npm//:vite"}; !slices.Equal(got,
		want) {
		t.Errorf("manifestLabels = %v, want %v", got, want)
	}
	if got := l.manifestLabels(nil, ""); got != nil {
		t.Errorf("manifestLabels(nil) = %v, want nil", got)
	}
}

// The root manifest read from a package below it: the member's link target is
// spelled from the test's package, not the manifest's, so it stays the root's.
func TestManifestLabels_RootManifestFromBelow(t *testing.T) {
	root, l := npmRepo(t)
	writeFile(t, filepath.Join(root, "package.json"),
		`{"name": "monorepo", "dependencies": {"@acme/lib": "workspace:*"}}`)
	m := nearestManifest(root, "worker/test/deep")
	if got := l.manifestLabels(m, "worker/test/deep"); !slices.Equal(got,
		[]string{"//:node_modules/@acme/lib"}) {
		t.Errorf("from worker/test/deep: %v, want the root's link target", got)
	}
	if got := l.manifestLabels(m, ""); !slices.Equal(got,
		[]string{":node_modules/@acme/lib"}) {
		t.Errorf("from the root: %v, want the link target in the package", got)
	}
}

func TestManifestProgramDoesNotDropExportedJavaScriptAndSeparateTypes(t *testing.T) {
	root := writeTree(t, map[string]string{
		"lib/package.json":         `{"name":"@test/lib","types":"./types/index.d.ts","exports":{".":{"types":"./types/index.d.ts","import":"./src/index.mjs"},"./styles.css":"./generated/styles.css"}}`,
		"lib/types/index.d.ts":     `export declare const value: number;`,
		"lib/src/index.mjs":        `export { value } from "./value.mjs";`,
		"lib/src/value.mjs":        `export const value = 42;`,
		"lib/generated/styles.css": `body { color: red; }`,
	})
	tree := generateAll(t, root)
	got := generatedRule(tree.results["lib"], "lib")
	if got == nil {
		t.Fatal("manifest source package has no generated compile")
	}
	for _, src := range []string{"package.json", "types/index.d.ts", "src/index.mjs", "src/value.mjs", "generated/styles.css"} {
		if !slices.Contains(got.AttrStrings("srcs"), src) {
			t.Errorf("missing exported source or resource %s: %v", src, got.AttrStrings("srcs"))
		}
	}
	if got.AttrString("tsconfig") != "" || generatedRule(tree.results["lib"], "tsconfig") != nil {
		t.Fatal("manifest source package invented a tsconfig")
	}
}

func TestGeneratedPackageMovesExportToCanonicalChildWithoutAlias(t *testing.T) {
	requireTsgo(t)
	root := writeTree(t, map[string]string{
		"package.json":         convergePlainPkg,
		"parent/tsconfig.json": `{"include":["*.ts"]}`,
		"parent/index.ts":      `export const value=1;`,
		"parent/BUILD.bazel": `exports_files(["kept.json", "test/fixtures/value.json"], visibility=["//consumer:__pkg__"])
exports_files(["test/fixtures/public.json"], licenses=["notice"])`,
		"parent/kept.json":                 `{}`,
		"parent/test/tsconfig.json":        `{"include":["*.ts"]}`,
		"parent/test/value.ts":             `export const value=2;`,
		"parent/test/fixtures/value.json":  `{}`,
		"parent/test/fixtures/public.json": `{}`,
	})
	captureLog(t, func() { convergeGazelle(t, root) })
	parent := buildFileText(t, root, "parent")
	child := buildFileText(t, root, "parent/test")
	if strings.Contains(parent, "test/fixtures/value.json") || !strings.Contains(parent, "kept.json") {
		t.Fatalf("parent export ownership stale:\n%s", parent)
	}
	if !strings.Contains(child, `"fixtures/value.json"`) || !strings.Contains(child, `visibility = ["//consumer:__pkg__"]`) {
		t.Fatalf("missing canonical child export/visibility:\n%s", child)
	}
	file, err := rule.LoadFile(filepath.Join(root, "parent/test/BUILD.bazel"), "parent/test")
	if err != nil {
		t.Fatal(err)
	}
	public := false
	for _, r := range file.Rules {
		if r.Kind() == "exports_files" && slices.Contains(r.AttrStrings("licenses"), "notice") {
			public = true
			if r.Attr("visibility") != nil {
				t.Fatal("default-public export visibility narrowed")
			}
		}
	}
	if !public {
		t.Fatal("export licenses lost")
	}
	captureLog(t, func() { convergeGazelle(t, root) })
	if next := buildFileText(t, root, "parent/test"); next != child {
		t.Fatalf("export relocation unstable:\n%s", lineDiff(child, next))
	}
}
