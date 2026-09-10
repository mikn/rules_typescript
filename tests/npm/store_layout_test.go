package npm_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The features lockfile resolves ansi-styles@6.2.3 twice, once per peer set:
// two store trees, two keys, each holding real files only, and beside each a
// relative link to the ansi-regex its snapshot names. The hidden hoist links
// one ansi-regex: app-a's, the first importer whose direct deps claim the name.
func TestStoreTreesAndLinks(t *testing.T) {
	tree := verify.New(t)

	for _, c := range []struct{ peers, want string }{
		{"ansi-regex@5.0.1", "5.0.1"},
		{"ansi-regex@6.2.2", "6.2.2"},
	} {
		token := strings.NewReplacer("@", "_", ".", "_").Replace(c.peers)
		store := tree.FoundDir("*/features/node_modules/.pnpm/ansi-styles@6.2.3_"+
			token+"_*/node_modules/ansi-styles", "*/node_modules/ansi-styles/*")
		store.File("package.json").Contains(`"version": "6.2.3"`)
		requireNoSymlinkInside(t, store.Abs())

		link := filepath.Join(filepath.Dir(store.Abs()), "ansi-regex")
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("%s: not a symlink: %v", link, err)
			continue
		}
		if want := "../../" + c.peers + "/node_modules/ansi-regex"; target != want {
			t.Errorf("%s -> %s, want %s", link, target, want)
		}
		if got := version(t, filepath.Join(link, "package.json")); got != c.want {
			t.Errorf("%s resolves to ansi-regex %s, want %s", link, got, c.want)
		}
	}

	hoisted := tree.Path(
		"tests/npm/features/node_modules/.pnpm/node_modules/ansi-regex")
	target, err := os.Readlink(hoisted)
	if err != nil {
		t.Fatalf("%s: not a symlink: %v", hoisted, err)
	}
	if want := "../ansi-regex@5.0.1/node_modules/ansi-regex"; target != want {
		t.Errorf("hidden hoist %s -> %s, want %s", hoisted, target, want)
	}
}

func version(t *testing.T, manifest string) string {
	t.Helper()
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Errorf("%s: %v", manifest, err)
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Errorf("%s: %v", manifest, err)
	}
	return pkg.Version
}

// The runfiles tree expands a tree artifact into one symlink per file, so the
// tree itself is where the manifest's symlink resolves.
func requireNoSymlinkInside(t *testing.T, dir string) {
	t.Helper()
	manifest, err := filepath.EvalSymlinks(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	real := filepath.Dir(manifest)
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			t.Errorf("%s: a symlink inside a store tree", path)
		}
		return nil
	}
	if err := filepath.WalkDir(real, walk); err != nil {
		t.Fatalf("walking %s: %v", real, err)
	}
}
