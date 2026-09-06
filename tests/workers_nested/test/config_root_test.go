package nested_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The root and the pool layer's call are lines of the generated config; a test
// under it only shows what they resolve, so the lines themselves are pinned.
func TestTheRootIsTheConfigsPackage(t *testing.T) {
	tree := verify.New(t)

	tree.File("tests/workers_nested/test/_worker_test_vitest.config.mjs").Contains(
		`import { workersPoolLayer } from './_worker_test_workers_pool.mjs';`,
		`root: resolve(process.env.TS_TEST_PACKAGE_DIR, ".."),`,
		`cacheDir: resolve(process.env.TEST_TMPDIR, '.vite')`,
		`workersPoolLayer(bazelLayer, user, resolve(process.env.TS_TEST_PACKAGE_DIR, "../../.."))`,
		`merge(merge(bazelLayer, user), attrLayer)`,
	)

	// The same-package control: the config beside the tests keeps the test's
	// package as the root.
	tree.File("tests/workers/_worker_test_vitest.config.mjs").Contains(
		`import { workersPoolLayer } from './_worker_test_workers_pool.mjs';`,
		`root: resolve(process.env.TS_TEST_PACKAGE_DIR, "."),`,
		`workersPoolLayer(bazelLayer, user, resolve(process.env.TS_TEST_PACKAGE_DIR, "../.."))`,
	)
}
