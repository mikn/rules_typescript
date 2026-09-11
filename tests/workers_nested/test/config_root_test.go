package nested_test

import (
	"testing"

	"github.com/mikn/rules_typescript/tests/verify"
)

// The root and the workspace directory are lines of the generated config; a
// test under it only shows what they resolve, so the lines are pinned.
func TestTheRootIsTheConfigsPackage(t *testing.T) {
	tree := verify.New(t)

	tree.File("tests/workers_nested/test/_worker_test.vitest/config.mjs").Contains(
		`from '../vitest.config.mjs';`,
		`const ROOT = resolve(HERE, "..");`,
		`const WORKSPACE_DIR = resolve(HERE, "../../..");`,
		`cacheDir: resolve(process.env.TEST_TMPDIR, '.vite')`,
		`merge(merge(bazelLayer, user), providerLayer)`,
	)

	// The same-package control: the config beside the tests keeps the test's
	// package as the root.
	tree.File("tests/workers/_worker_test.vitest/config.mjs").Contains(
		`const ROOT = resolve(HERE, ".");`,
		`const WORKSPACE_DIR = resolve(HERE, "../..");`,
	)
}
