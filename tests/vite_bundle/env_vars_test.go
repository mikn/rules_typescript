package vite_bundle_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// env_vars substitution happens at bundle time, so the value has to be inlined
// as a literal and the import.meta.env reference has to be gone.
func TestEnvVarSubstitution(t *testing.T) {
	js := verify.New(t).File("tests/vite_bundle/entry_vite_env_bundle/entry_env.es.js")
	js.Contains("https://api.example.com")
	js.Excludes("import.meta.env.VITE_API_URL", verify.PlaceholderBundle)
}
