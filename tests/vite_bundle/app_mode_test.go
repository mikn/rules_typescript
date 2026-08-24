package vite_bundle_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// mode = "app" produces a directory of HTML plus assets, not a single JS file.
func TestAppModeOutput(t *testing.T) {
	dir := verify.New(t).Dir("tests/vite_bundle/entry_vite_app_bundle")
	dir.Glob("*.js")

	for _, html := range dir.Glob("*.html") {
		if html.Size() == 0 {
			t.Errorf("%s is empty", html.Name())
		}
	}
}
