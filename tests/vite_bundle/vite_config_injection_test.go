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

// composed_config.ts is TypeScript and gets its plugin from a sibling it imports
// with an extensionless relative specifier. Both are why staging exists: a .ts
// config cannot be reached by a dynamic import, so the generated config goes
// through Vite's own loader, and the sibling resolves only because
// vite_config_srcs staged it beside the copy under bazel-bin. Finding the
// sentinel means the whole chain held, and the source content beside it means
// Bazel's own wiring survived it.
func TestComposedTypeScriptUserConfig(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/entry_vite_composed_config_bundle/entry.es.js")
	js.Contains("_COMPOSED_TS_CONFIG_LOADED", "add", "PI")
	js.Excludes(placeholder)
}
