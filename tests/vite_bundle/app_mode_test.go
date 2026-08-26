package vite_bundle_test

import (
	"path"
	"regexp"
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

var hashedChunk = regexp.MustCompile(`^index-[A-Za-z0-9_-]+\.js$`)

// mode = "app" produces a directory of HTML plus assets, not a single JS file.
// The chunk carries a content hash and the HTML has to name that hash: an
// unrewritten script src is a bundle that cannot load.
func TestAppModeOutput(t *testing.T) {
	dir := verify.New(t).Dir("tests/vite_bundle/entry_vite_app_bundle")

	js := dir.Glob("*.js")
	if len(js) != 1 {
		t.Fatalf("want exactly one .js in the bundle, got %d", len(js))
	}
	chunk := path.Base(js[0].Name())
	if !hashedChunk.MatchString(chunk) {
		t.Errorf("%s is not a hashed filename, want index-<hash>.js", chunk)
	}

	for _, html := range dir.Glob("*.html") {
		if html.Size() == 0 {
			t.Errorf("%s is empty", html.Name())
		}
		html.Contains("/assets/" + chunk)
	}
}
