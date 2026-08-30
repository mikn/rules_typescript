package typescript

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStripJSONComments_LineComment(t *testing.T) {
	got := string(stripJSONComments([]byte("{\n  // a comment\n  \"a\": 1\n}")))
	want := "{\n  \n  \"a\": 1\n}"
	if got != want {
		t.Errorf("stripJSONComments: got %q, want %q", got, want)
	}
}

func TestStripJSONComments_BlockComment(t *testing.T) {
	got := string(stripJSONComments([]byte(`{/* hi */"a": 1}`)))
	if got != `{"a": 1}` {
		t.Errorf("stripJSONComments: got %q", got)
	}
}

func TestStripJSONComments_PreservesSequencesInsideStrings(t *testing.T) {
	in := `{"url": "https://example.com//x", "glob": "src/**/*", "block": "/* not a comment */"}`
	if got := string(stripJSONComments([]byte(in))); got != in {
		t.Errorf("stripJSONComments altered string literals:\n got %s\nwant %s", got, in)
	}
}

func TestStripJSONComments_PreservesEscapedQuote(t *testing.T) {
	in := `{"a": "he said \"//\"", "b": 1}`
	if got := string(stripJSONComments([]byte(in))); got != in {
		t.Errorf("stripJSONComments altered escaped string:\n got %s\nwant %s", got, in)
	}
}

func TestStripJSONComments_TrailingCommas(t *testing.T) {
	var v struct {
		A []int          `json:"a"`
		B map[string]int `json:"b"`
	}
	if err := unmarshalJSONC([]byte("{\"a\": [1, 2,],\n \"b\": {\"k\": 1,},\n}"), &v); err != nil {
		t.Fatalf("unmarshalJSONC: %v", err)
	}
	if !reflect.DeepEqual(v.A, []int{1, 2}) || v.B["k"] != 1 {
		t.Errorf("decoded %+v", v)
	}
}

func TestStripJSONComments_UnterminatedBlockComment(t *testing.T) {
	// Must not panic or loop forever; the truncated input stays invalid JSON.
	var v map[string]any
	if err := unmarshalJSONC([]byte(`{"a": 1 /* oops`), &v); err == nil {
		t.Error("expected an error for unterminated block comment")
	}
}

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

// A generated tsconfig carries one paths entry per npm package, pointing under
// npm_dir. Those must not become path aliases: doing so resolved
// `import 'zod'` to //.bazel/npm/zod/index.d instead of @npm//:zod.
func TestLoadTsConfigPaths_SkipsToolManagedDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsconfig.json")
	body := `{
  "compilerOptions": {
    "paths": {
      "zod": ["./.bazel/npm/zod/index.d.ts"],
      "zod/*": ["./.bazel/npm/zod/*"],
      "@/*": ["src/*"]
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	got := loadTsConfigPaths(path, "")
	want := map[string]string{"@/": "src/"}
	if len(got) != len(want) {
		t.Fatalf("loadTsConfigPaths: got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias %q: got %q, want %q", k, got[k], v)
		}
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

// A tsconfig paths value is a fallback chain: TypeScript tries each entry in
// turn. Gazelle emits one directory per alias, so it has to pick the entry a
// specifier actually resolves through rather than whichever is written first.
func TestLoadTsConfigPaths_FallbackChains(t *testing.T) {
	tests := []struct {
		name    string
		dirs    []string
		paths   string
		want    map[string]string
		wantLog bool
	}{
		{
			name:  "output tree mirror is never the alias",
			dirs:  []string{"src/api", "bazel-bin/src/api"},
			paths: `"@api/*": ["./src/api/*", "./bazel-bin/src/api/*"]`,
			want:  map[string]string{"@api/": "src/api/"},
		},
		{
			name:  "first entry missing, second on disk",
			dirs:  []string{"generated/api"},
			paths: `"@api/*": ["./src/api/*", "./generated/api/*"]`,
			want:  map[string]string{"@api/": "generated/api/"},
		},
		{
			name:    "two real directories keep the first and report the rest",
			dirs:    []string{"src/api", "generated/api"},
			paths:   `"@api/*": ["./src/api/*", "./generated/api/*"]`,
			want:    map[string]string{"@api/": "src/api/"},
			wantLog: true,
		},
		{
			name:  "no entry on disk keeps the first",
			paths: `"@api/*": ["./src/api/*", "./generated/api/*"]`,
			want:  map[string]string{"@api/": "src/api/"},
		},
		{
			name:  "tool-managed chain drops the alias",
			dirs:  []string{".bazel/npm/zod", "bazel-bin/.bazel/npm/zod"},
			paths: `"zod/*": ["./.bazel/npm/zod/*", "./bazel-bin/.bazel/npm/zod/*"]`,
			want:  nil,
		},
		{
			name:    "output-tree-only chain drops the alias and says so",
			dirs:    []string{"bazel-bin/src/api"},
			paths:   `"@api/*": ["./bazel-bin/src/api/*"]`,
			want:    nil,
			wantLog: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "tsconfig.json")
			body := "{\"compilerOptions\": {\"paths\": {" + tc.paths + "}}}"
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}

			var logs bytes.Buffer
			restore := log.Writer()
			log.SetOutput(&logs)
			got := loadTsConfigPaths(path, "")
			log.SetOutput(restore)

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("loadTsConfigPaths: got %v, want %v", got, tc.want)
			}
			if gotLog := logs.Len() > 0; gotLog != tc.wantLog {
				t.Errorf("logged %v, want %v: %s", gotLog, tc.wantLog, logs.String())
			}
		})
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
