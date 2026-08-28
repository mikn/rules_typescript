package npm_publish_test

import (
	"archive/tar"
	"io"
	"os"
	"sort"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Main    string `json:"main"`
	Types   string `json:"types"`
}

func TestStagingDirectoryAndTarball(t *testing.T) {
	tree := verify.New(t)

	pkg := tree.Dir("tests/npm_publish/math_pkg_pkg/package")
	pkg.Exists()
	for _, required := range []string{"package.json", "math.js", "math.js.map", "math.d.ts"} {
		pkg.File(required).Exists()
	}

	var manifest packageJSON
	pkg.File("package.json").JSON(&manifest)
	if manifest.Version != "1.0.0" {
		t.Errorf("package.json version is %q, want 1.0.0", manifest.Version)
	}

	tarball := tree.File("tests/npm_publish/math_pkg_pkg.tar")
	got := tarEntries(t, tarball)
	for _, required := range []string{
		"package/package.json", "package/math.js", "package/math.js.map", "package/math.d.ts",
	} {
		if !got[required] {
			t.Errorf("%s does not hold %s; it holds:\n  %v", tarball.Name(), required, sortedKeys(got))
		}
	}
}

// A template with no line breaks must still get its version stamped and stay
// valid JSON.
func TestCompactTemplate(t *testing.T) {
	tree := verify.New(t)

	var manifest packageJSON
	tree.File("tests/npm_publish/compact_pkg_pkg/package/package.json").JSON(&manifest)

	if manifest.Version != "2.5.0" {
		t.Errorf("compact template version is %q, want 2.5.0", manifest.Version)
	}
	if manifest.Name != "@rules_typescript/test-compact" {
		t.Errorf("compact template lost its name: %q", manifest.Name)
	}
	// main/types are absent from the template and must be filled in from the
	// compiled outputs.
	if manifest.Main != "./math.js" {
		t.Errorf("compact template main is %q, want ./math.js", manifest.Main)
	}
}

func tarEntries(t *testing.T, f verify.File) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	if !f.Exists() {
		return names
	}
	fh, err := os.Open(f.Abs())
	if err != nil {
		t.Errorf("%s: %v", f.Name(), err)
		return names
	}
	defer fh.Close()
	r := tar.NewReader(fh)
	for {
		hdr, err := r.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Errorf("%s: %v", f.Name(), err)
			return names
		}
		names[hdr.Name] = true
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
