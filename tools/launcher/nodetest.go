package main

import (
	"fmt"
	"os"
)

// planNodeTest names every file on the command line, so unlike the vitest plan
// there is no staged root to keep the runner from globbing a sibling out of bin.
func planNodeTest(
	cfg *Config, r *Resolver, plan *Plan, args []string,
) (*Plan, error) {
	n := cfg.NodeTest
	plan.Dir = r.Dir()

	// A coverage run asks for a report node --test cannot write, and an empty
	// one would read as a clean run.
	if os.Getenv("COVERAGE_DIR") != "" {
		return nil, fmt.Errorf(
			"ts_test: the node:test runner does not report coverage. " +
				"Run `bazel coverage` against vitest targets, or `bazel test` " +
				"against this one.")
	}

	shard, err := shardFiles(r, n.TestFilesList)
	if err != nil {
		return nil, err
	}
	if len(shard) == 0 {
		plan.ExitEarly = true
		plan.Messages = append(plan.Messages, fmt.Sprintf(
			"ts_test: no test files assigned to shard %d/%d", shardIndex(), totalShards()))
		return plan, nil
	}

	root := r.Dir()
	if root == "" {
		root, err = os.MkdirTemp(os.Getenv("TEST_TMPDIR"), "ts_test_root")
		if err != nil {
			return nil, err
		}
	}
	_, err = installNodeModules(r, plan, root, cfg.Workspace, n.NodeModules)
	if err != nil {
		return nil, err
	}

	argv, err := runtimeCommand(cfg, r)
	if err != nil {
		return nil, err
	}
	if n.ResolveHook != "" {
		hook, err := r.Path(n.ResolveHook)
		if err != nil {
			return nil, err
		}
		// --import, not NODE_OPTIONS: node --test runs each file in a child
		// process and forwards the parent's execArgv to it.
		argv = append(argv, "--import", hook)
	}
	// The entry keeps its runfiles path, so relative reads land where the
	// checkout has them; node --test forwards execArgv to its children.
	argv = append(argv, "--preserve-symlinks-main")
	argv = append(argv, args...)
	argv = append(argv, "--test")

	// --test_filter reaches a test runner as TESTBRIDGE_TEST_ONLY; node:test
	// takes it as a regular expression over test names.
	if only := os.Getenv("TESTBRIDGE_TEST_ONLY"); only != "" {
		argv = append(argv, "--test-name-pattern", only)
	}

	for _, f := range shard {
		argv = append(argv, f.path)
	}
	plan.Argv = argv
	return plan, nil
}
