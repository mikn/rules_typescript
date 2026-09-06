package typescript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadTsConfigPaths_JSONC covers the real-world failure: tsconfig.json is
// JSONC, and comments used to make path aliases silently disappear.
func TestLoadTsConfigPaths_JSONC(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
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
`
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadTsConfigPaths(path, "")
	want := map[string]string{
		"@/":           "src/",
		"@components/": "src/components/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}

func TestLoadTsConfigPaths_AliasValueWithDoubleSlash(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
  "compilerOptions": {
    "paths": {
      "cdn": ["https://cdn.example.com//assets"] // not a comment
    }
  }
}
`
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadTsConfigPaths(path, "")
	if got["cdn"] != "https://cdn.example.com//assets" {
		t.Errorf("alias value mangled: got %q", got["cdn"])
	}
}

func TestLoadTsConfigPaths_SkipsFirstPartyPackageSelfEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	body := `{
  "compilerOptions": {
    "paths": {
      "packages/widget": ["./packages/widget/index"],
      "packages/widget/*": ["./packages/widget/*"],
      "@acme/ui": ["./packages/ui/index"],
      "@/*": ["src/*"]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	got := loadTsConfigPaths(path, "")
	want := map[string]string{"@acme/ui": "packages/ui/index", "@/": "src/"}
	if len(got) != len(want) {
		t.Fatalf("loadTsConfigPaths: got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias %q: got %q, want %q", k, got[k], v)
		}
	}
}

// A tsconfig's `paths` are relative to that tsconfig, and its targets become
// Bazel labels, which are relative to the repo root. Those are the same thing
// only when the tsconfig is at the repo root -- which is the shape of every
// example in this repo and of almost no monorepo, where the app is a workspace
// member in a subdirectory.
func TestLoadTsConfigPaths_TargetsAreRelativeToTheRepoRootNotTheTsConfig(t *testing.T) {
	repo := t.TempDir()
	app := filepath.Join(repo, "web")
	if err := os.MkdirAll(filepath.Join(app, "shared"), 0o750); err != nil {
		t.Fatal(err)
	}
	tsconfig := `{
  "compilerOptions": {
    "paths": {
      "#shared/*": ["./shared/*"],
      "@platform/auth": ["./lib/auth/platform-adapter.ts"]
    }
  }
}`
	path := filepath.Join(app, "tsconfig.json")
	if err := os.WriteFile(path, []byte(tsconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadTsConfigPaths(path, "web")
	want := map[string]string{
		"#shared/":       "web/shared/",
		"@platform/auth": "web/lib/auth/platform-adapter.ts",
	}
	if len(got) != len(want) {
		t.Fatalf("loadTsConfigPaths: got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias %q = %q, want %q", k, got[k], v)
		}
	}
}

// A baseUrl of "." is the shape most hand-written tsconfigs carry; the value is
// a repo-relative path for label construction, so no "./" prefix survives it.
func TestLoadTsConfigPaths_BaseUrlDotWritesNoDotSlash(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"],
      "@lib/*": ["src/lib/*"],
      "@entry": ["./src/index"]
    }
  }
}
`
	path := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(path, []byte(tsconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadTsConfigPaths(path, "")
	want := map[string]string{
		"@/":     "src/",
		"@lib/":  "src/lib/",
		"@entry": "src/index",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadTsConfigPaths: got %v, want %v", got, want)
	}
}
