package attrs_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// reporters and coverage thresholds have no observable effect from inside a
// passing test, so the generated config itself is what gets pinned here.
func TestEveryAttributeReachesTheGeneratedConfig(t *testing.T) {
	tree := verify.New(t)

	tree.File("tests/vitest/attrs/_attrs_test_vitest.config.mjs").Contains(
		`environment: "node"`,
		`setupFiles: [abs("./setup.js")]`,
		`globals: true`,
		`reporters: ["default"]`,
		`coverage: { provider: "v8", thresholds: { "lines": 0, "perFile": true } }`,
		`{"test":{"testTimeout":20000}}`,
		`merge(merge(bazelLayer, user), attrLayer)`,
	)
}
