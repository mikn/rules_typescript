package typescript

import (
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

	got := loadTsConfigPaths(path)
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

	got := loadTsConfigPaths(path)
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

	got := loadTsConfigPaths(path)
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
