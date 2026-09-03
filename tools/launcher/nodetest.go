package main

import (
	"fmt"
	"os"
)

// planNodeTest names every file on the command line, so unlike the vitest plan
// there is no staged root to keep the runner from globbing a sibling out of bin.
func planNodeTest(cfg *Config, r *Resolver, plan *Plan) (*Plan, error) {
	n := cfg.NodeTest
	plan.Dir = r.Dir()

	// A coverage run asks for a file node --test has nothing to write into, and
	// an empty lcov would read as a clean run.
	if out := os.Getenv("COVERAGE_OUTPUT_FILE"); out != "" {
		return nil, fmt.Errorf(
			"ts_test: runner \"%s\" does not report coverage. Run `bazel coverage` "+
				"against vitest targets, or `bazel test` against this one.", RunnerNodeTest)
	}

	if _, err := installNodeModules(r, plan, n.NodeModules); err != nil {
		return nil, err
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
