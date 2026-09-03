package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

func nodeTestFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	return fakeRunfiles(t, map[string]string{
		"_main/tests/app/app_test_files.txt": strings.Join([]string{
			"_main/tests/app/a.test.js",
			"_main/tests/app/b.test.js",
			"_main/tests/app/c.test.js",
		}, "\n"),
		"_main/tests/app/a.test.js":                           "x",
		"_main/tests/app/b.test.js":                           "x",
		"_main/tests/app/c.test.js":                           "x",
		"_main/tests/app/_app_test_node_modules/node_modules": dirMarker,
		"_main/ts/private/node_test_hook.mjs":                 "export {}",
		"+node+/bin/node":                                     "#!/bin/sh\n",
	})
}

func nodeTestConfig() *Config {
	return &Config{
		Label:     "//tests/app:app_test",
		Mode:      ModeNodeTest,
		Workspace: "_main",
		Runtime:   "+node+/bin/node",
		NodeTest: &NodeTestConfig{
			TestFilesList: "_main/tests/app/app_test_files.txt",
			NodeModules:   "_main/tests/app/_app_test_node_modules/node_modules",
			ResolveHook:   "_main/ts/private/node_test_hook.mjs",
		},
	}
}

func TestPlanNodeTestRunsEveryTestFileUnderTheToolchainNode(t *testing.T) {
	r, real := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		real["+node+/bin/node"],
		"--import", real["_main/ts/private/node_test_hook.mjs"],
		"--test",
		real["_main/tests/app/a.test.js"],
		real["_main/tests/app/b.test.js"],
		real["_main/tests/app/c.test.js"],
	}
	if !slices.Equal(plan.Argv, want) {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
	if !plan.UseExec {
		t.Error("nothing post-processes a node:test run, so the launcher should exec into node")
	}
}

// The hook has to reach the children node --test spawns, which inherit the
// parent's execArgv but not a flag the launcher appended after the file list.
func TestPlanNodeTestPutsTheResolveHookBeforeTheTestFlag(t *testing.T) {
	r, _ := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	hook := slices.Index(plan.Argv, "--import")
	test := slices.Index(plan.Argv, "--test")
	if hook < 0 || test < 0 || hook > test {
		t.Errorf("argv = %q, want --import before --test", plan.Argv)
	}
}

func TestPlanNodeTestOmitsTheHookWhenTheConfigHasNone(t *testing.T) {
	r, _ := nodeTestFixture(t)
	cfg := nodeTestConfig()
	cfg.NodeTest.ResolveHook = ""
	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plan.Argv, "--import") {
		t.Errorf("argv = %q, want no --import", plan.Argv)
	}
}

func TestPlanNodeTestPartitionsShards(t *testing.T) {
	r, real := nodeTestFixture(t)
	t.Setenv("TEST_TOTAL_SHARDS", "2")
	t.Setenv("TEST_SHARD_INDEX", "1")
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Argv, real["_main/tests/app/b.test.js"]) {
		t.Errorf("shard 1 of 2 should run b.test.js, got %q", plan.Argv)
	}
	if slices.Contains(plan.Argv, real["_main/tests/app/a.test.js"]) {
		t.Errorf("shard 1 of 2 should not run a.test.js, got %q", plan.Argv)
	}
}

func TestPlanNodeTestExitsCleanlyOnAnEmptyShard(t *testing.T) {
	r, _ := nodeTestFixture(t)
	t.Setenv("TEST_TOTAL_SHARDS", "8")
	t.Setenv("TEST_SHARD_INDEX", "7")
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ExitEarly {
		t.Fatal("an empty shard must not start node")
	}
}

func TestPlanNodeTestForwardsTestFilterAsANamePattern(t *testing.T) {
	r, _ := nodeTestFixture(t)
	t.Setenv("TESTBRIDGE_TEST_ONLY", "bumps twice")
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(plan.Argv, "--test-name-pattern")
	if i < 0 || plan.Argv[i+1] != "bumps twice" {
		t.Errorf("argv = %q, want --test-name-pattern 'bumps twice'", plan.Argv)
	}
}

func TestPlanNodeTestOmitsTheNamePatternWithoutAFilter(t *testing.T) {
	r, _ := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plan.Argv, "--test-name-pattern") {
		t.Errorf("argv = %q, want no --test-name-pattern", plan.Argv)
	}
}

// Silently handing Bazel an empty lcov would report full coverage loss as a
// clean run, so the plan refuses instead.
func TestPlanNodeTestRefusesACoverageRun(t *testing.T) {
	r, _ := nodeTestFixture(t)
	t.Setenv("COVERAGE_OUTPUT_FILE", filepath.Join(t.TempDir(), "out.dat"))
	_, err := MakePlan(nodeTestConfig(), r, nil)
	if err == nil || !strings.Contains(err.Error(), "does not report coverage") {
		t.Fatalf("want a coverage refusal naming the runner, got %v", err)
	}
}

// A bare specifier resolves only through the node_modules link at the runfiles
// root, which is what ESM's upward walk reaches; NODE_PATH answers CJS only.
func TestPlanNodeTestExposesTheNpmTreeToEsmResolution(t *testing.T) {
	_, real := nodeTestFixture(t)
	const rlocation = "_main/tests/app/_app_test_node_modules/node_modules"
	tree := real[rlocation]
	r, root := withRunfilesDir(t, tree, rlocation)
	plan, err := MakePlan(nodeTestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.EnvOverrides["NODE_PATH"], tree) {
		t.Errorf("NODE_PATH = %q, want it to start with %q", plan.EnvOverrides["NODE_PATH"], tree)
	}
	got, err := os.Readlink(filepath.Join(root, "node_modules"))
	if err != nil {
		t.Fatalf("no node_modules link at the runfiles root: %v", err)
	}
	if got != tree {
		t.Errorf("link -> %q, want the npm tree %q", got, tree)
	}
}

// withRunfilesDir rebuilds the resolver with RUNFILES_DIR at the fixture's tree
// root: fakeRunfiles clears it, and a resolver reads it once, at construction.
func withRunfilesDir(t *testing.T, real, rlocation string) (*Resolver, string) {
	t.Helper()
	root := strings.TrimSuffix(filepath.ToSlash(real), "/"+rlocation)
	t.Setenv("RUNFILES_DIR", root)
	r, err := newResolver(runfiles.ManifestFile(os.Getenv("RUNFILES_MANIFEST_FILE")))
	if err != nil {
		t.Fatal(err)
	}
	return r, root
}

func TestParseConfigRejectsANodeTestSectionWithoutAFileList(t *testing.T) {
	_, err := ParseConfig([]byte(`{"mode":"node_test","node_test":{}}`))
	if err == nil || !strings.Contains(err.Error(), "test_files_list") {
		t.Fatalf("want an error naming test_files_list, got %v", err)
	}
}
