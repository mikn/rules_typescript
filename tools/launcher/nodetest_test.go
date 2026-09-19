package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func nodeTestFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	return fakeRunfiles(t, map[string]string{
		"_main/tests/app/app_test_files.txt": strings.Join([]string{
			"_main/tests/app/a.test.js",
			"_main/tests/app/b.test.js",
			"_main/tests/app/c.test.js",
		}, "\n"),
		"_main/tests/app/a.test.js":                 "x",
		"_main/tests/app/b.test.js":                 "x",
		"_main/tests/app/c.test.js":                 "x",
		"_main/tests/app/node_modules/zod/index.js": "x",
		"_main/ts/private/node_test_hook.mjs":       "export {}",
		"+node+/bin/node":                           "#!/bin/sh\n",
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
			NodeModules:   []string{"_main/tests/app/node_modules"},
			ResolveHook:   "_main/ts/private/node_test_hook.mjs",
		},
	}
}

func TestPlanNodeTestRunsEveryTestFileUnderTheToolchainNode(t *testing.T) {
	r, real := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		real["+node+/bin/node"],
		"--import", real["_main/ts/private/node_test_hook.mjs"],
		"--preserve-symlinks-main",
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
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	hook := slices.Index(plan.Argv, "--import")
	test := slices.Index(plan.Argv, "--test")
	if hook < 0 || test < 0 || hook > test {
		t.Errorf("argv = %q, want --import before --test", plan.Argv)
	}
}

// The entry keeps its runfiles path, so a test's relative reads land where
// the checkout has them; the flag rides execArgv into node --test's children.
func TestPlanNodeTestKeepsTheEntryAtItsRunfilesPath(t *testing.T) {
	r, _ := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	flag := slices.Index(plan.Argv, "--preserve-symlinks-main")
	test := slices.Index(plan.Argv, "--test")
	if flag < 0 || test < 0 || flag > test {
		t.Errorf("argv = %q, want --preserve-symlinks-main before --test", plan.Argv)
	}
}

// node reads its flags up to the first file, so the target's args go between
// the launcher's own flags and --test, where the children inherit them too.
func TestPlanNodeTestPutsArgsBeforeTheTestFlag(t *testing.T) {
	r, real := nodeTestFixture(t)
	args := []string{"--experimental-test-module-mocks"}
	plan, err := MakePlan(nodeTestConfig(), r, args, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	flag := slices.Index(plan.Argv, args[0])
	test := slices.Index(plan.Argv, "--test")
	hook := slices.Index(plan.Argv, "--import")
	if flag < 0 || !(hook < flag && flag < test) {
		t.Errorf("argv = %q, want %q between --import and --test", plan.Argv, args[0])
	}
	if slices.Index(plan.Argv, real["_main/tests/app/a.test.js"]) < test {
		t.Errorf("argv = %q, want the files after --test", plan.Argv)
	}
}

func TestPlanNodeTestOmitsTheHookWhenTheConfigHasNone(t *testing.T) {
	r, _ := nodeTestFixture(t)
	cfg := nodeTestConfig()
	cfg.NodeTest.ResolveHook = ""
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plan.Argv, "--import") {
		t.Errorf("argv = %q, want no --import", plan.Argv)
	}
}

func TestPlanNodeTestPartitionsShards(t *testing.T) {
	r, real := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Index: 1, Total: 2})
	if err != nil {
		t.Fatal(err)
	}
	files := plan.Argv[slices.Index(plan.Argv, "--test")+1:]
	if !slices.Equal(files, []string{real["_main/tests/app/b.test.js"]}) {
		t.Errorf("shard 1/2 files = %q; want only b.test.js", files)
	}
}

func TestPlanNodeTestExitsCleanlyOnAnEmptyShard(t *testing.T) {
	r, _ := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Index: 7, Total: 8})
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
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
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
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
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
	t.Setenv("COVERAGE_DIR", t.TempDir())
	_, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
	if err == nil || !strings.Contains(err.Error(), "does not report coverage") {
		t.Fatalf("want a coverage refusal naming the runner, got %v", err)
	}
}

// The resolve hook reads NODE_PATH for the importer's directory; without a
// runfiles tree it is staged from the manifest and linked in at the root.
func TestPlanNodeTestNamesTheImportersNodeModulesOnNodePath(t *testing.T) {
	r, real := nodeTestFixture(t)
	plan, err := MakePlan(nodeTestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	const importer = "/_main/tests/app/node_modules"
	nodePath := plan.EnvOverrides["NODE_PATH"]
	dir := strings.Split(nodePath, string(os.PathListSeparator))[0]
	if !strings.HasSuffix(dir, filepath.FromSlash(importer)) {
		t.Fatalf("NODE_PATH = %q, want the importer's staged directory first", dir)
	}
	got, err := filepath.EvalSymlinks(filepath.Join(dir, "zod", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	zod := real["_main/tests/app/node_modules/zod/index.js"]
	if want, _ := filepath.EvalSymlinks(zod); got != want {
		t.Errorf("zod/index.js resolves to %q, want %q", got, want)
	}
	root := strings.TrimSuffix(dir, filepath.FromSlash(importer))
	link := filepath.Join(root, "_main", "node_modules")
	if target, err := os.Readlink(link); err != nil || target != dir {
		t.Errorf("%s -> %q, %v; want the importer's %q", link, target, err, dir)
	}
}

func TestParseConfigRejectsANodeTestSectionWithoutAFileList(t *testing.T) {
	_, err := ParseConfig([]byte(`{"mode":"node_test","node_test":{}}`))
	if err == nil || !strings.Contains(err.Error(), "test_files_list") {
		t.Fatalf("want an error naming test_files_list, got %v", err)
	}
}

func TestRunAdvertisesShardingOnlyWhenExecuting(t *testing.T) {
	for _, mode := range []string{"run", "dump flag", "dump environment"} {
		for _, badPath := range []bool{false, true} {
			name := mode
			if badPath {
				name += " with invalid status path"
			}
			t.Run(name, func(t *testing.T) {
				nodeTestFixture(t)
				config, err := json.Marshal(nodeTestConfig())
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "launcher.json")
				if err := os.WriteFile(path, config, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv(ConfigEnvVar, path)
				t.Setenv("COVERAGE_DIR", "")
				t.Setenv("TEST_TOTAL_SHARDS", "8")
				t.Setenv("TEST_SHARD_INDEX", "7")
				status := filepath.Join(t.TempDir(), "status")
				if badPath {
					status = filepath.Join(status, "missing")
				}
				t.Setenv("TEST_SHARD_STATUS_FILE", status)
				t.Setenv(DumpEnvVar, "")
				args := os.Args
				t.Cleanup(func() { os.Args = args })
				os.Args = []string{args[0]}
				if mode == "dump flag" {
					os.Args = append(os.Args, DumpFlag)
				} else if mode == "dump environment" {
					t.Setenv(DumpEnvVar, "1")
				}
				want := 0
				if mode == "run" && badPath {
					want = 1
				}
				if got := run(); got != want {
					t.Fatalf("run() = %d, want %d", got, want)
				}
				_, err = os.Stat(status)
				if mode == "run" && !badPath {
					if err != nil {
						t.Fatal(err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("unexpected status file: %v", err)
				}
			})
		}
	}
}
