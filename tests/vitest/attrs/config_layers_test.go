package attrs_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The provider has no observable effect from inside a passing test, so the
// generated config itself is what gets pinned here, with the layer order.
func TestTheGeneratedConfigLayersBazelUserProviderAndSnapshots(t *testing.T) {
	tree := verify.New(t)

	config := tree.File("tests/vitest/attrs/_attrs_test_vitest.config.mjs")
	config.Contains(
		`import { workersPoolLayer } from './_attrs_test_workers_pool.mjs';`,
		`from './attrs_test/vitest.config.mts';`,
		`root: resolve(process.env.TS_TEST_PACKAGE_DIR, "."),`,
		`const providerLayer = { test: { coverage: { provider: "v8" } } };`,
		`merge(merge(merge(bazelLayer, user), providerLayer), snapshotLayer)`,
		`const merged = setupFilesInRoot(withCompiledSetup(`,
		`withCompiledSetup(merge(bazelLayer, p))) : p,`,
	)
	config.Excludes(
		"attrLayer", "environment:", "setupFiles: [abs(", "globals: true",
	)
}
