package npm_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// beside is the link a package's own edge is: the entry `name` in the store
// directory holding the package's tree, found from the package's realpath.
func beside(t *testing.T, pkg, name string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(pkg)
	if err != nil {
		t.Fatalf("realpath of %s: %v", pkg, err)
	}
	return filepath.Join(filepath.Dir(real), name)
}

// versionOf reads a package directory's manifest, through the link.
func versionOf(t *testing.T, pkg string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pkg, "package.json"))
	if err != nil {
		t.Fatalf("%s: %v", pkg, err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("%s/package.json: %v", pkg, err)
	}
	return manifest.Version
}
