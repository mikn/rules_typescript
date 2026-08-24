package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

// fakeRunfiles lays out files on disk and returns a resolver backed only by a
// manifest — the layout that broke every hand-rolled ${SCRIPT}.runfiles guess.
func fakeRunfiles(t *testing.T, entries map[string]string) (*Resolver, map[string]string) {
	t.Helper()
	root := t.TempDir()
	real := map[string]string{}
	lines := []string{}
	for rlocation, content := range entries {
		path := filepath.Join(root, "files", filepath.FromSlash(rlocation))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if content == dirMarker {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		real[rlocation] = path
		lines = append(lines, rlocation+" "+path)
	}
	manifest := filepath.Join(root, "MANIFEST")
	if err := os.WriteFile(manifest, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNFILES_DIR", "")
	t.Setenv("TEST_SRCDIR", "")
	t.Setenv("RUNFILES_MANIFEST_FILE", manifest)
	r, err := newResolver(runfiles.ManifestFile(manifest))
	if err != nil {
		t.Fatal(err)
	}
	return r, real
}

const dirMarker = "\x00dir"

func TestResolverWorksFromAManifestAlone(t *testing.T) {
	r, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/main.js":      "console.log(1)",
		"+npm+npm/node/bin/node":       "#!/bin/sh\n",
		"_main/tests/app/node_modules": dirMarker,
	})

	if r.Dir() != "" {
		t.Errorf("Dir() = %q, want empty: there is no runfiles directory in manifest mode", r.Dir())
	}
	for rlocation, want := range real {
		got, err := r.Path(rlocation)
		if err != nil {
			t.Fatalf("Path(%q): %v", rlocation, err)
		}
		if got != want {
			t.Errorf("Path(%q) = %q, want %q", rlocation, got, want)
		}
	}
}

func TestResolverInTreeJoinsUnderADirectoryArtifact(t *testing.T) {
	r, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/node_modules": dirMarker,
	})
	got, err := r.InTree("_main/tests/app/node_modules", "vite/bin/vite.js")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(real["_main/tests/app/node_modules"], "vite", "bin", "vite.js")
	if got != want {
		t.Errorf("InTree = %q, want %q", got, want)
	}
}

func TestResolverReportsMissingEntries(t *testing.T) {
	r, _ := fakeRunfiles(t, map[string]string{"_main/a.js": "x"})
	if _, err := r.Path("_main/nope.js"); err == nil {
		t.Fatal("Path succeeded for a path that is not in the manifest")
	} else if !strings.Contains(err.Error(), "_main/nope.js") {
		t.Errorf("error does not name the path: %v", err)
	}
}

func TestResolverEnvCarriesTheManifestToChildren(t *testing.T) {
	r, _ := fakeRunfiles(t, map[string]string{"_main/a.js": "x"})
	joined := strings.Join(r.Env(), " ")
	if !strings.Contains(joined, "RUNFILES_MANIFEST_FILE=") {
		t.Errorf("Env() = %v, want it to carry RUNFILES_MANIFEST_FILE", r.Env())
	}
}
