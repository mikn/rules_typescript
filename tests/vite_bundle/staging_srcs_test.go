package vite_bundle_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// staging_mock_plugin.mjs emits the sentinel only when VITE_STAGING_ROOT is set
// and the staged files are present inside it.
func TestStagingSrcs(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/entry_vite_staging_bundle/entry.es.js")
	js.Contains("_STAGING_ROOT_WAS_SET = true", "add", "PI")
	js.Excludes(verify.PlaceholderBundle)
}
