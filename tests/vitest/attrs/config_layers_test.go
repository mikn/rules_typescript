package attrs_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The provider has no observable effect from inside a passing test, so the
// generated config itself is what gets pinned here, with the layer order.
func TestTheGeneratedConfigLayersBazelUserProviderAndSnapshots(t *testing.T) {
	tree := verify.New(t)

	config := tree.File("tests/vitest/attrs/_attrs_test.vitest/config.mjs")
	config.Contains(
		`from './vitest.config.mts';`,
		`const ROOT = resolve(HERE, ".");`,
		`name: 'rules_typescript:module-ids',`,
		`server: { fs: { allow: FS_ALLOW } },`,
		`const providerLayer = { test: { coverage: { provider: "v8" } } };`,
		`merge(merge(merge(bazelLayer, user), providerLayer), snapshotLayer)`,
		`const merged = withCompiledRun(merge(`,
		`withCompiledRun(merge(bazelLayer, p)) : p,`,
	)
	config.Excludes(
		"attrLayer", "environment:", "setupFiles: [abs(", "globals: true",
	)
}

// The run is one pattern over the root, not the list of the compiled test
// files, so vitest collects it by one crawl whatever the count of files.
func TestTheGeneratedConfigCollectsTheRunByOnePattern(t *testing.T) {
	tree := verify.New(t)

	config := tree.File("tests/vitest/attrs/_attrs_test.vitest/config.mjs")
	config.Contains(
		`const INCLUDE = ["**/*.{test,spec}.{js,jsx,mjs,cjs}"];`,
		`const BIN_PROBE = "tests/vitest/attrs/attrs.test.js";`,
		`const test = { ...config.test, include: INCLUDE };`,
	)
	config.Excludes(
		`"attrs.test.js"]`, "include: INCLUDE,", "for (const f of INCLUDE)",
	)
}
