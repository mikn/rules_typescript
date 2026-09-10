package typescript

import (
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
// devDependencies, lockfile-gated, in D7 spelling; a member's name is its view.
func TestManifestLabels_Union(t *testing.T) {
	root, l := npmRepo(t)
	writeFile(t, filepath.Join(root, "workers/download/package.json"), `{
		"name": "download",
		"dependencies": {"@acme/lib": "workspace:*", "left-pad": "1.3.0"},
		"devDependencies": {"wrangler": "4.118.0", "typescript": "5.9.2"}
	}`)
	var got []string
	out := captureLog(t, func() {
		got = l.manifestLabels(nearestManifest(root, "workers/download/test"))
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
	got := l.manifestLabels(nearestManifest(root, "scripts"))
	if want := []string{"@npm//:typescript", "@npm//:vite"}; !slices.Equal(got,
		want) {
		t.Errorf("manifestLabels = %v, want %v", got, want)
	}
	if got := l.manifestLabels(nil); got != nil {
		t.Errorf("manifestLabels(nil) = %v, want nil", got)
	}
}
