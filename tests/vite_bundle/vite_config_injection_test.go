package vite_bundle_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

const (
	injected    = "_VITE_PLUGIN_INJECTED"
	placeholder = "Placeholder bundle"
)

// mock_plugin.mjs prepends the sentinel from renderChunk. Finding it proves the
// user-supplied plugin ran; finding the source content alongside it proves
// outDir, resolve.alias and entry-point rewriting still work.
func TestLibModeUserPlugin(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/entry_vite_with_config_bundle/entry.es.js")
	js.Contains(injected, "add", "PI")
	js.Excludes(placeholder)
}

func TestAppModeUserPlugin(t *testing.T) {
	verify.New(t).
		Dir("tests/vite_bundle/entry_vite_app_with_config_bundle").
		AnyContains("*.js", injected)
}
