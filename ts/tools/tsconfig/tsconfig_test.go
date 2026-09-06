package tsconfig

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(prev); log.SetFlags(flags) }()
	fn()
	return buf.String()
}

func mustResolve(t *testing.T, path string) *Resolved {
	t.Helper()
	r, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", path, err)
	}
	return r
}

// A config that only extends a shared base still carries the base's paths, and
// the base wrote them relative to itself.
func TestResolve_ExtendsBase(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, "packages/tsconfig-base/tsconfig.json")
	write(t, base, `{"compilerOptions": {"paths": {"@/*": ["src/*"]}}}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	write(t, leaf, `{"extends": "../../packages/tsconfig-base/tsconfig.json"}`)

	got := mustResolve(t, leaf)
	if want := map[string][]string{"@/*": {"src/*"}}; !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v", got.Paths, want)
	}
	if want := filepath.Dir(base); got.PathsDir != want {
		t.Errorf("PathsDir = %q, want the base's directory %q", got.PathsDir, want)
	}
}

// tsc replaces an inherited compilerOptions key wholesale, so a leaf that
// redeclares paths drops the base's other entries rather than merging them.
func TestResolve_LeafReplacesTheWholeKey(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "packages/tsconfig-base/tsconfig.json"),
		`{"compilerOptions": {"paths": {"@/*": ["src/*"], "@lib/*": ["lib/*"]}}}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	write(t, leaf, `{
  "extends": "../../packages/tsconfig-base/tsconfig.json",
  "compilerOptions": {"paths": {"@/*": ["app/*"]}}
}`)

	got := mustResolve(t, leaf)
	if want := map[string][]string{"@/*": {"app/*"}}; !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v", got.Paths, want)
	}
	if want := filepath.Dir(leaf); got.PathsDir != want {
		t.Errorf("PathsDir = %q, want the leaf's directory %q", got.PathsDir, want)
	}
}

func TestResolve_ExtendsArrayLastWins(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "apps/web/a.json"),
		`{"compilerOptions": {"paths": {"@/*": ["a/*"], "@only-a/*": ["only/*"]}}}`)
	write(t, filepath.Join(repo, "apps/web/b.json"),
		`{"compilerOptions": {"paths": {"@/*": ["b/*"]}}}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	write(t, leaf, `{"extends": ["./a", "./b.json"]}`)

	got := mustResolve(t, leaf)
	if want := map[string][]string{"@/*": {"b/*"}}; !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v", got.Paths, want)
	}
}

// baseUrl and paths can come from different files in the chain, and each is
// relative to the file that wrote it.
func TestResolve_BaseURLAndPathsKeepTheirWritersDirectories(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, "packages/tsconfig-base/tsconfig.json")
	write(t, base, `{"compilerOptions": {"baseUrl": "src"}}`)
	leaf := filepath.Join(repo, "apps/web/tsconfig.json")
	write(t, leaf, `{
  "extends": "../../packages/tsconfig-base/tsconfig.json",
  "compilerOptions": {"paths": {"@/*": ["./*"]}}
}`)

	got := mustResolve(t, leaf)
	if got.BaseURL != "src" || got.BaseURLDir != filepath.Dir(base) {
		t.Errorf("BaseURL = %q in %q, want \"src\" in %q", got.BaseURL, got.BaseURLDir, filepath.Dir(base))
	}
	if got.PathsDir != filepath.Dir(leaf) {
		t.Errorf("PathsDir = %q, want %q", got.PathsDir, filepath.Dir(leaf))
	}
}

// A bare or scoped specifier resolves through node_modules, which the reader
// has no root for; the rest of the config still loads, and the skip is said
// once per specifier.
func TestResolve_PackageFormExtendsIsSkippedAndSaidOnce(t *testing.T) {
	repo := t.TempDir()
	leaf := filepath.Join(repo, "tsconfig.json")
	write(t, leaf, `{
  "extends": "@tsconfig/said-once/tsconfig.json",
  "compilerOptions": {"paths": {"@/*": ["src/*"]}}
}`)

	var got *Resolved
	first := captureLog(t, func() { got = mustResolve(t, leaf) })
	if want := map[string][]string{"@/*": {"src/*"}}; !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v", got.Paths, want)
	}
	if !strings.Contains(first, `"@tsconfig/said-once/tsconfig.json"`) {
		t.Errorf("the first read said nothing about the skipped specifier; log was %q", first)
	}
	if again := captureLog(t, func() { mustResolve(t, leaf) }); again != "" {
		t.Errorf("the second read repeated the warning: %q", again)
	}
}

func TestResolve_CycleTerminates(t *testing.T) {
	repo := t.TempDir()
	leaf := filepath.Join(repo, "tsconfig.json")
	write(t, leaf, `{"extends": "./ring.json"}`)
	write(t, filepath.Join(repo, "ring.json"), `{
  "extends": "./tsconfig.json",
  "compilerOptions": {"paths": {"@/*": ["src/*"]}}
}`)

	done := make(chan *Resolved, 1)
	go func() {
		captureLog(t, func() { done <- mustResolve(t, leaf) })
	}()
	select {
	case got := <-done:
		if want := map[string][]string{"@/*": {"src/*"}}; !reflect.DeepEqual(got.Paths, want) {
			t.Errorf("Paths = %v, want %v", got.Paths, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Resolve did not terminate on an extends cycle")
	}
}

// Two branches of an extends array reaching the same base is not a cycle: the
// base is read again, because where it lands in the merge order is what
// decides whether it beats a config declared between the two branches.
func TestResolve_DiamondReReadsTheSharedBase(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "shared.json"), `{"compilerOptions": {"paths": {"@/*": ["shared/*"]}}}`)
	write(t, filepath.Join(repo, "a.json"), `{"extends": "./shared.json"}`)
	write(t, filepath.Join(repo, "b.json"), `{"extends": "./shared.json"}`)
	write(t, filepath.Join(repo, "middle.json"), `{"compilerOptions": {"paths": {"@/*": ["middle/*"]}}}`)
	leaf := filepath.Join(repo, "tsconfig.json")
	write(t, leaf, `{"extends": ["./a.json", "./middle.json", "./b.json"]}`)

	got := mustResolve(t, leaf)
	if want := map[string][]string{"@/*": {"shared/*"}}; !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v", got.Paths, want)
	}
}

// tsconfig.json is JSONC; a comment or a trailing comma must not cost a key.
func TestResolve_JSONC(t *testing.T) {
	leaf := filepath.Join(t.TempDir(), "tsconfig.json")
	write(t, leaf, `{
  // Comments are legal in tsconfig.json.
  "compilerOptions": {
    /* baseUrl anchors the paths below. */
    "baseUrl": "src",
    "paths": {
      "@/*": ["./*"], // wildcard alias
      "@components/*": ["components/*"],
    },
  },
}
`)

	got := mustResolve(t, leaf)
	want := map[string][]string{"@/*": {"./*"}, "@components/*": {"components/*"}}
	if got.BaseURL != "src" || !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("BaseURL = %q, Paths = %v; want \"src\", %v", got.BaseURL, got.Paths, want)
	}
}

// include or files anywhere in the chain names the program's inputs; without
// either, tsc enumerates the directory tree.
func TestResolve_InputsComeFromAnyFileInTheChain(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "base.json"), `{"include": ["src"]}`)
	write(t, filepath.Join(repo, "options.json"), `{"compilerOptions": {"strict": true}}`)
	write(t, filepath.Join(repo, "from-base.json"), `{"extends": "./base.json"}`)
	write(t, filepath.Join(repo, "empty-files.json"), `{"extends": "./options.json", "files": []}`)
	write(t, filepath.Join(repo, "none.json"), `{"extends": "./options.json"}`)

	for name, want := range map[string]bool{"from-base.json": true, "empty-files.json": true, "none.json": false} {
		if got := mustResolve(t, filepath.Join(repo, name)).Inputs; got != want {
			t.Errorf("%s: Inputs = %v, want %v", name, got, want)
		}
	}
}

func TestResolve_JsxImportSourceLeafWins(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "base.json"), `{"compilerOptions": {"jsxImportSource": "react"}}`)
	write(t, filepath.Join(repo, "inherits.json"), `{"extends": "./base.json"}`)
	write(t, filepath.Join(repo, "overrides.json"),
		`{"extends": "./base.json", "compilerOptions": {"jsxImportSource": "preact"}}`)

	for name, want := range map[string]string{"inherits.json": "react", "overrides.json": "preact"} {
		if got := mustResolve(t, filepath.Join(repo, name)).JsxImportSource; got != want {
			t.Errorf("%s: JsxImportSource = %q, want %q", name, got, want)
		}
	}
}

// The leaf's own failure is the caller's to handle; a base that cannot be read
// is said and skipped, because the leaf still describes a program.
func TestResolve_LeafFailsBaseIsSkipped(t *testing.T) {
	repo := t.TempDir()
	if _, err := Resolve(filepath.Join(repo, "missing.json")); err == nil {
		t.Error("Resolve(missing.json): want an error")
	}
	broken := filepath.Join(repo, "broken.json")
	write(t, broken, `{"compilerOptions": {`)
	if _, err := Resolve(broken); err == nil || !strings.Contains(err.Error(), broken) {
		t.Errorf("Resolve(broken.json) = %v, want an error naming the file", err)
	}

	leaf := filepath.Join(repo, "tsconfig.json")
	write(t, leaf, `{"extends": ["./broken.json", "./gone.json"], "compilerOptions": {"paths": {"@/*": ["src/*"]}}}`)
	var got *Resolved
	logged := captureLog(t, func() { got = mustResolve(t, leaf) })
	if want := map[string][]string{"@/*": {"src/*"}}; !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("Paths = %v, want %v", got.Paths, want)
	}
	for _, spec := range []string{`"./broken.json"`, `"./gone.json"`} {
		if !strings.Contains(logged, spec) {
			t.Errorf("the skipped base %s is not in the log %q", spec, logged)
		}
	}
}

// One specifier or, since TypeScript 5.0, an array of them; and "types": []
// is not the same as no "types" key at all.
func TestRead_ExtendsFormsAndTypesPointer(t *testing.T) {
	repo := t.TempDir()
	cases := map[string]struct {
		body    string
		extends Extends
		types   *[]string
	}{
		"single.json": {`{"extends": "./base", "compilerOptions": {"types": []}}`, Extends{"./base"}, &[]string{}},
		"array.json":  {`{"extends": ["./a", "./b"], "compilerOptions": {"types": ["node"]}}`, Extends{"./a", "./b"}, &[]string{"node"}},
		"none.json":   {`{"compilerOptions": {}}`, nil, nil},
	}
	for name, tc := range cases {
		path := filepath.Join(repo, name)
		write(t, path, tc.body)
		got, err := Read(path)
		if err != nil {
			t.Fatalf("Read(%s): %v", name, err)
		}
		if !reflect.DeepEqual(got.Extends, tc.extends) {
			t.Errorf("%s: Extends = %v, want %v", name, got.Extends, tc.extends)
		}
		if !reflect.DeepEqual(got.CompilerOptions.Types, tc.types) {
			t.Errorf("%s: Types = %v, want %v", name, got.CompilerOptions.Types, tc.types)
		}
	}
}

func TestResolveExtends(t *testing.T) {
	dir := filepath.Join(string(filepath.Separator), "repo", "apps", "web")
	cases := []struct {
		spec string
		want string
		ok   bool
	}{
		{"", "", false},
		{"./base", filepath.Join(dir, "base.json"), true},
		{"../shared/tsconfig.json", filepath.Join(dir, "..", "shared", "tsconfig.json"), true},
		{"/etc/ts/base", "/etc/ts/base.json", true},
		{"@tsconfig/resolve-extends/tsconfig.json", "", false},
	}
	for _, tc := range cases {
		var got string
		var ok bool
		captureLog(t, func() { got, ok = ResolveExtends(dir, tc.spec) })
		if got != tc.want || ok != tc.ok {
			t.Errorf("ResolveExtends(%q) = %q, %v; want %q, %v", tc.spec, got, ok, tc.want, tc.ok)
		}
	}
}
