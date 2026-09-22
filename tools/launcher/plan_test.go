package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

func TestPlanNodeExecsTheToolchainRuntime(t *testing.T) {
	r, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/main.js": "x",
		"+node+/bin/node":         "#!/bin/sh\n",
	})
	cfg := &Config{
		Label:   "//tests/app:app",
		Mode:    ModeNode,
		Runtime: "+node+/bin/node",
		RunArgs: []string{"--experimental-vm-modules"},
		Node:    &NodeConfig{Entry: "_main/tests/app/main.js"},
	}
	plan, err := MakePlan(cfg, r, []string{"--flag", "a b"}, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		real["+node+/bin/node"], "--experimental-vm-modules",
		real["_main/tests/app/main.js"], "--flag", "a b",
	}
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
	if !plan.UseExec {
		t.Error("a plain binary should exec, leaving no launcher in the process tree")
	}
}

func TestPlanNodeFallsBackToSystemNode(t *testing.T) {
	r, _ := fakeRunfiles(t, map[string]string{"_main/a.js": "x"})
	cfg := &Config{Mode: ModeNode, Node: &NodeConfig{Entry: "_main/a.js"}}
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Argv[0] != "node" {
		t.Errorf("argv[0] = %q, want the system node fallback", plan.Argv[0])
	}
}

// Without a runfiles tree the importer's node_modules is staged from the
// manifest into a directory of the plan's own, which NODE_PATH names.
func TestPlanNodeAddsNodeModulesToNodePath(t *testing.T) {
	r, real := fakeRunfiles(t, map[string]string{
		"_main/a.js":                            "x",
		"_main/tests/node_modules/zod/index.js": "x",
	})
	t.Setenv("NODE_PATH", "/pre-existing")
	cfg := &Config{Mode: ModeNode, Node: &NodeConfig{
		Entry:       "_main/a.js",
		NodeModules: "_main/tests/node_modules",
	}}
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	nodePath := plan.EnvOverrides["NODE_PATH"]
	entries := strings.Split(nodePath, string(os.PathListSeparator))
	importer := filepath.FromSlash("/_main/tests/node_modules")
	if len(entries) != 2 || entries[1] != "/pre-existing" ||
		!strings.HasSuffix(entries[0], importer) {
		t.Fatalf("NODE_PATH = %q, want the staged importer before /pre-existing",
			nodePath)
	}
	got, err := filepath.EvalSymlinks(filepath.Join(entries[0], "zod", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	zod := real["_main/tests/node_modules/zod/index.js"]
	if want, _ := filepath.EvalSymlinks(zod); got != want {
		t.Errorf("zod/index.js resolves to %q, want %q", got, want)
	}
}

func TestPlanNodeLinksOptionalDepsIntoATempTree(t *testing.T) {
	r, real := fakeRunfiles(t, map[string]string{
		"_main/a.js":                      "x",
		"+npm+/oxlint_linux/package.json": "{}",
		"+npm+/scoped/package.json":       "{}",
	})
	cfg := &Config{Mode: ModeNode, Node: &NodeConfig{
		Entry: "_main/a.js",
		OptionalDeps: []PackageLink{
			{Name: "oxlint-linux-x64", PackageJSON: "+npm+/oxlint_linux/package.json"},
			{Name: "@oxc/linux-x64", PackageJSON: "+npm+/scoped/package.json"},
		},
	}}
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()

	if plan.UseExec {
		t.Error("a plan with a temp tree must not exec: nothing would clean the tree up")
	}
	root := plan.EnvOverrides["NODE_PATH"]
	if root == "" {
		t.Fatal("NODE_PATH was not pointed at the temp tree")
	}
	root = strings.Split(root, string(os.PathListSeparator))[0]
	for name, pkg := range map[string]string{
		"oxlint-linux-x64": "+npm+/oxlint_linux/package.json",
		"@oxc/linux-x64":   "+npm+/scoped/package.json",
	} {
		got, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, _ := filepath.EvalSymlinks(filepath.Dir(real[pkg]))
		if got != want {
			t.Errorf("%s resolves to %q, want %q", name, got, want)
		}
	}

	tmp := root
	plan.Cleanup()
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s behind", tmp)
	}
}

func vitestFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	return fakeRunfiles(t, map[string]string{
		"_main/tests/app/_app.vitest/config.mjs": "export default {}",
		"_main/tests/app/app_test_files.txt": strings.Join([]string{
			"_main/tests/app/a.test.js",
			"_main/tests/app/b.test.js",
			"_main/tests/app/c.test.js",
		}, "\n"),
		"_main/tests/app/a.test.js":                      "x",
		"_main/tests/app/b.test.js":                      "x",
		"_main/tests/app/c.test.js":                      "x",
		"_main/tests/app/node_modules":                   dirMarker,
		"_main/tests/app/node_modules/vitest/vitest.mjs": "x",
		"+node+/bin/node":                                "#!/bin/sh\n",
	})
}

func vitestConfig() *Config {
	return &Config{
		Label:     "//tests/app:app_test",
		Mode:      ModeVitest,
		Workspace: "_main",
		Runtime:   "+node+/bin/node",
		Vitest: &VitestConfig{
			VitestInTree:  "vitest/vitest.mjs",
			ConfigFile:    "_main/tests/app/_app_vitest.config.mjs",
			TestFilesList: "_main/tests/app/app_test_files.txt",
			NodeModules:   []string{"_main/tests/app/node_modules"},
			Stage: map[string]string{
				"_main/tests/app/_app.vitest/config.mjs": "_main/tests/app/" +
					"_app_vitest.config.mjs",
			},
		},
	}
}

// treeRoot is the tree plan.Dir, the config's package, sits under.
func treeRoot(plan *Plan) string {
	return strings.TrimSuffix(plan.Dir, filepath.FromSlash("/_main/tests/app"))
}

// vitest collects the run by its own glob over the root: the launcher names
// no file, so nothing is matched file by file against a list of arguments.
func TestPlanVitestHandsVitestNoFile(t *testing.T) {
	r, real := vitestFixture(t)
	t.Setenv("COVERAGE_DIR", "")
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	tree := treeRoot(plan)
	want := []string{
		real["+node+/bin/node"],
		filepath.Join(tree, "_main/tests/app/node_modules/vitest/vitest.mjs"),
		"run", "--config",
		filepath.Join(tree, "_main/tests/app/_app_vitest.config.mjs"),
	}
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
	if plan.UseExec {
		t.Error("the test runner has to outlive vitest to post-process coverage")
	}
}

// The target's args follow the launcher's own flags.
func TestPlanVitestPutsArgsAfterItsFlags(t *testing.T) {
	r, _ := vitestFixture(t)
	t.Setenv("COVERAGE_DIR", t.TempDir())
	args := []string{"--reporter=dot"}
	plan, err := MakePlan(vitestConfig(), r, args, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	config := slices.Index(plan.Argv, "--config")
	coverage := slices.Index(plan.Argv, "--coverage.enabled")
	last := len(plan.Argv) - 1
	if !(0 < config && config < coverage && coverage < last) ||
		plan.Argv[last] != args[0] {
		t.Errorf("argv = %q, want %q last, after --config and the coverage flags",
			plan.Argv, args[0])
	}
}

func TestPlanVitestRejectsChangedInsteadOfPassingWithZeroTests(t *testing.T) {
	for _, args := range [][]string{
		{"--changed"},
		{"--changed", "HEAD~1"},
		{"--changed=HEAD~1"},
		{"a.test.js", "--changed"},
	} {
		for _, shard := range []Shard{{Total: 1}, {Index: 3, Total: 4}} {
			t.Run(strings.Join(args, " ")+"/"+strconv.Itoa(shard.Index), func(t *testing.T) {
				r, _ := vitestFixture(t)
				plan, err := MakePlan(vitestConfig(), r, args, shard)
				if err == nil {
					if plan.Cleanup != nil {
						plan.Cleanup()
					}
					t.Fatal("--changed must fail even when this shard has no tests")
				}
				if !strings.Contains(err.Error(), "Git source history") || !strings.Contains(err.Error(), "compiled-file filter") {
					t.Fatalf("error must explain the unsupported selection and alternative: %v", err)
				}
			})
		}
	}
}

func TestPlanVitestPreservesExplicitFiltersAndPositionalChanged(t *testing.T) {
	for _, args := range [][]string{
		{"a.test.js", "-t", "adds numbers"},
		{"--testNamePattern=adds numbers"},
		{"--changedness"},
		{"--", "--changed"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r, _ := vitestFixture(t)
			plan, err := MakePlan(vitestConfig(), r, args, Shard{Total: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Cleanup()
			if !slices.Equal(plan.Argv[len(plan.Argv)-len(args):], args) {
				t.Fatalf("argv = %q, want unchanged filter suffix %q", plan.Argv, args)
			}
		})
	}
}

func TestShardFiles(t *testing.T) {
	files := []testFile{{"c", "/third"}, {"a", "/first"}, {"b", "/second"}}
	original := slices.Clone(files)
	for _, classes := range [][][]string{
		{{"a", "b", "c"}}, {{"a", "c"}, {"b"}}, {{"a"}, {"b"}, {"c"}, {}, {}},
	} {
		union := []testFile{}
		for index, want := range classes {
			selected := shardFiles(files, Shard{Index: index, Total: len(classes)})
			union = append(union, selected...)
			got := []string{}
			for _, file := range selected {
				got = append(got, file.rlocation)
			}
			if !slices.Equal(got, want) {
				t.Errorf("shard %d/%d: got %v, want %v", index, len(classes), got, want)
			}
		}
		slices.SortFunc(union, func(a, b testFile) int { return strings.Compare(a.rlocation, b.rlocation) })
		if !slices.Equal(union, []testFile{files[1], files[2], files[0]}) {
			t.Errorf("%d shards: union = %v", len(classes), union)
		}
	}
	if !slices.Equal(files, original) || len(shardFiles(nil, Shard{Total: 1})) != 0 {
		t.Fatal("selection must preserve input and leave an empty input empty")
	}
}

func TestParseShard(t *testing.T) {
	for _, tc := range []struct {
		index, total string
		want         Shard
	}{
		{"", "", Shard{Total: 1}}, {"0", "1", Shard{Total: 1}}, {"2", "3", Shard{Index: 2, Total: 3}},
	} {
		if got, err := parseShard(tc.index, tc.total); err != nil || got != tc.want {
			t.Errorf("parseShard(%q, %q) = %v, %v", tc.index, tc.total, got, err)
		}
	}
	for _, values := range [][2]string{{"x", "1"}, {"0", "x"}, {"0", "0"}, {"-1", "2"}, {"2", "2"}} {
		if _, err := parseShard(values[0], values[1]); err == nil {
			t.Errorf("parseShard(%q, %q) must reject invalid shard settings", values[0], values[1])
		}
	}
}

func TestPlanVitestPartitionsShards(t *testing.T) {
	r, _ := vitestFixture(t)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Index: 1, Total: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	files, err := os.ReadDir(filepath.Join(plan.EnvOverrides["TS_TEST_FILES_ROOT"], "_main/tests/app"))
	files = slices.DeleteFunc(files, func(file os.DirEntry) bool { return !strings.HasSuffix(file.Name(), ".test.js") })
	if err != nil || len(files) != 1 || files[0].Name() != "b.test.js" {
		t.Fatalf("staged files = %v, %v; want only b.test.js", files, err)
	}
	if slices.ContainsFunc(plan.Argv, func(arg string) bool { return strings.HasPrefix(arg, "--shard") }) {
		t.Error("Vitest must not partition the staged files again")
	}
}

func TestPlanVitestLeavesEmptySuitePolicyToVitest(t *testing.T) {
	for _, total := range []int{1, 3} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			r, real := vitestFixture(t)
			if err := os.WriteFile(real["_main/tests/app/app_test_files.txt"], nil, 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{"--passWithNoTests=false"}
			plan, err := MakePlan(vitestConfig(), r, args, Shard{Total: total})
			if err != nil {
				t.Fatal(err)
			}
			if plan.ExitEarly {
				t.Fatal("globally empty suite must reach Vitest's no-tests policy")
			}
			defer plan.Cleanup()
			if !slices.Contains(plan.Argv, args[0]) || !slices.Contains(plan.Argv, "--config") {
				t.Errorf("Vitest must receive its config and arguments: %q", plan.Argv)
			}
		})
	}
}

func TestPlanVitestExitsCleanlyOnAnEmptyShard(t *testing.T) {
	r, _ := vitestFixture(t)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Index: 7, Total: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ExitEarly {
		t.Fatal("an empty shard must not start vitest")
	}
	if len(plan.Argv) != 0 {
		t.Errorf("argv = %q, want none for an empty shard", plan.Argv)
	}
	if !slices.ContainsFunc(plan.Messages, func(m string) bool {
		return strings.Contains(m, "no test files assigned to shard 7/8")
	}) {
		t.Errorf("messages = %q, want the empty-shard line", plan.Messages)
	}
}

// collect_coverage.sh merges the .dat files under COVERAGE_DIR into
// COVERAGE_OUTPUT_FILE with the rule's merger; the launcher writes the former.
func TestPlanVitestWritesItsLcovUnderCoverageDir(t *testing.T) {
	_, real := vitestFixture(t)
	r := runfilesTree(t, real)
	dir := filepath.Join(t.TempDir(), "coverage")
	out := filepath.Join(t.TempDir(), "coverage.dat")
	t.Setenv("COVERAGE_DIR", dir)
	t.Setenv("COVERAGE_OUTPUT_FILE", out)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Argv, " ")
	reports := filepath.Join(dir, "vitest")
	if !strings.Contains(joined, "--coverage.reportsDirectory "+reports) {
		t.Errorf("argv %q is missing the reports directory %q", joined, reports)
	}
	if plan.PostRun == nil {
		t.Fatal("coverage runs need the lcov post-processing step")
	}
	if err := os.MkdirAll(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reports, "lcov.info"),
		[]byte("SF:a.js\nDA:1,1\nend_of_record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plan.PostRun(0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "vitest.dat"))
	if err != nil {
		t.Fatalf("the merger reads the .dat files under COVERAGE_DIR: %v", err)
	}
	want := "SF:tests/app/a.js\nDA:1,1\nend_of_record\n"
	if string(got) != want {
		t.Errorf("vitest.dat = %q, want %q", got, want)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("COVERAGE_OUTPUT_FILE is the merger's to write, got %v", err)
	}
}

// runfilesTree is the fixture as the directory a Linux test runs from, where
// vitest's root and the launcher's tree are one and the same runfiles tree.
func runfilesTree(t *testing.T, real map[string]string) *Resolver {
	t.Helper()
	const config = "_main/tests/app/_app.vitest/config.mjs"
	dir := strings.TrimSuffix(real[config], string(filepath.Separator)+
		filepath.FromSlash(config))
	r, err := directoryResolver(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPlanVitestFailsAPassingRunThatWroteNoLcov(t *testing.T) {
	r, _ := vitestFixture(t)
	t.Setenv("COVERAGE_DIR", t.TempDir())
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	if err := plan.PostRun(0); err == nil {
		t.Error("a passing run with no lcov.info would read as a clean report")
	}
	if err := plan.PostRun(1); err != nil {
		t.Errorf("a failed run has no report to write: %v", err)
	}
}

func TestRewriteLcovOnlyTouchesSourceFileLines(t *testing.T) {
	in := []byte("SF:_main/a.js\nFN:1,_main/x\nSF:_mainless/b.js\n")
	got := string(RewriteLcov(in, "_main", "/r", "/r"))
	want := "SF:a.js\nFN:1,_main/x\nSF:_mainless/b.js\n"
	if got != want {
		t.Errorf("RewriteLcov = %q, want %q", got, want)
	}
}

// istanbul writes paths relative to vite's root, the config's package, and a
// module a pool resolved through its realpath as an execroot path.
func TestRewriteLcovResolvesThePathsAgainstTheRoot(t *testing.T) {
	runDir := "/w/execroot/_main/bazel-out/k8-fastbuild/bin/tests/workers/t.runfiles"
	root := runDir + "/_main/tests/vitest/coverage"
	cases := []struct {
		name string
		in   string
		want string
	}{{
		name: "a file in the root's package",
		in:   "SF:same_package.js\n",
		want: "SF:tests/vitest/coverage/same_package.js\n",
	}, {
		name: "a file in an ancestor package",
		in:   "SF:../math.js\n",
		want: "SF:tests/vitest/math.js\n",
	}, {
		name: "escaping relative, as istanbul writes an out-of-root module",
		in: "SF:../../../../../../../../../../" +
			"bazel-out/k8-fastbuild/bin/tests/workers/src/index.js\n",
		want: "SF:tests/workers/src/index.js\n",
	}, {
		name: "absolute, under bazel-out",
		in:   "SF:/w/execroot/_main/bazel-out/k8-fastbuild/bin/tests/workers/src/index.js\n",
		want: "SF:tests/workers/src/index.js\n",
	}, {
		name: "absolute, elsewhere, is left alone",
		in:   "SF:/home/me/src/tests/workers/src/index.js\n",
		want: "SF:/home/me/src/tests/workers/src/index.js\n",
	}, {
		name: "a path into another repository's runfiles is left alone",
		in:   "SF:../../../../_other/x.js\n",
		want: "SF:../../../../_other/x.js\n",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RewriteLcov([]byte(tc.in), "_main", runDir, root))
			if got != tc.want {
				t.Errorf("RewriteLcov = %q, want %q", got, tc.want)
			}
		})
	}
}

func devServerFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	return fakeRunfiles(t, map[string]string{
		"_main/tests/app/dev_vite.config.mjs":           "export default {}",
		"_main/tests/app/node_modules":                  dirMarker,
		"_main/tests/app/node_modules/vite/bin/vite.js": "x",
		"_main/vite/vite_plugin_bazel.mjs":              "x",
		"_main/tools/native_server/serve":               "#!/bin/sh\n",
		"+node+/bin/node":                               "#!/bin/sh\n",
	})
}

// devServerWorkspace is a real directory, because planDevServer links the
// importer's node_modules into it.
func devServerWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", ws)
	return ws
}

func devServerConfig() *Config {
	return &Config{
		Label:   "//tests/app:dev",
		Mode:    ModeDevServer,
		Runtime: "+node+/bin/node",
		DevServer: &DevServerConfig{
			ConfigFile:      "_main/tests/app/dev_vite.config.mjs",
			PackageDir:      "tests/app",
			NodeModules:     "_main/tests/app/node_modules",
			ServerInTree:    "vite/bin/vite.js",
			Argv:            []string{"dev", "--config", "{config}"},
			RunsInJsRuntime: true,
			Plugin:          "_main/vite/vite_plugin_bazel.mjs",
			Port:            5173,
		},
	}
}

func TestPlanDevServerRunsViteFromTheNodeModulesTree(t *testing.T) {
	r, real := devServerFixture(t)
	ws := devServerWorkspace(t)
	plan, err := MakePlan(devServerConfig(), r, []string{"--host"}, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	vite := filepath.Join(real["_main/tests/app/node_modules"], "vite", "bin", "vite.js")
	// The port is passed on the command line even though the config carries it:
	// only the flag survives a server that does not read the config.
	want := []string{
		real["+node+/bin/node"], vite, "dev", "--config",
		real["_main/tests/app/dev_vite.config.mjs"], "--port", "5173", "--host",
	}
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
	if plan.Dir != filepath.Join(ws, "tests", "app") {
		t.Errorf("dir = %q, want the app package", plan.Dir)
	}
	if plan.EnvOverrides["BAZEL_BIN_DIR"] != filepath.Join(ws, "bazel-bin") {
		t.Errorf("BAZEL_BIN_DIR = %q", plan.EnvOverrides["BAZEL_BIN_DIR"])
	}
	if plan.EnvOverrides["VITE_PLUGIN_PATH"] != real["_main/vite/vite_plugin_bazel.mjs"] {
		t.Errorf("VITE_PLUGIN_PATH = %q", plan.EnvOverrides["VITE_PLUGIN_PATH"])
	}
	if !plan.Supervise.IgnoreTerm {
		t.Error("ibazel SIGTERMs the runner on rebuild; vite must survive it")
	}
}

func TestPlanDevServerExplainsAMissingVite(t *testing.T) {
	r, _ := devServerFixture(t)
	cfg := devServerConfig()
	cfg.DevServer.ServerInTree = "vite/bin/absent.js"
	_, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err == nil {
		t.Fatal("a node_modules without vite must fail")
	}
	if !strings.Contains(err.Error(), "node_modules() target") {
		t.Errorf("error is not actionable: %v", err)
	}
}

func nativeDevServerConfig() *Config {
	cfg := devServerConfig()
	cfg.DevServer.ServerInTree = ""
	cfg.DevServer.ServerBinary = "_main/tools/native_server/serve"
	cfg.DevServer.Argv = []string{"dev", "--config", "{config}", "{root}"}
	cfg.DevServer.RunsInJsRuntime = false
	return cfg
}

func TestPlanDevServerRunsANativeServerWithoutTheJsRuntime(t *testing.T) {
	r, real := devServerFixture(t)
	ws := devServerWorkspace(t)
	plan, err := MakePlan(nativeDevServerConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		real["_main/tools/native_server/serve"], "dev", "--config",
		real["_main/tests/app/dev_vite.config.mjs"], filepath.Join(ws, "tests", "app"), "--port", "5173",
	}
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
	// The plugin host is a Node process the server spawns by name, so a native
	// server that cannot see the toolchain node would fall back to a host one.
	nodeDir := filepath.Dir(real["+node+/bin/node"])
	if !strings.HasPrefix(plan.EnvOverrides["PATH"], nodeDir+string(os.PathListSeparator)) {
		t.Errorf("PATH = %q, want the toolchain node dir %q first",
			plan.EnvOverrides["PATH"], nodeDir)
	}
}

func TestPlanDevServerExplainsAMissingNodeModules(t *testing.T) {
	r, _ := devServerFixture(t)
	cfg := devServerConfig()
	cfg.DevServer.NodeModules = ""
	_, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err == nil || !strings.Contains(err.Error(), "node_modules") {
		t.Fatalf("want an actionable node_modules error, got %v", err)
	}
}

// Without a runfiles tree the files live in bazel-bin, beside every sibling
// target's identically named copy, which vitest's root would glob in.
func TestPlanVitestStagesAPrivateRootWithoutARunfilesDirectory(t *testing.T) {
	r, real := vitestFixture(t)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	root := treeRoot(plan)
	if root == "" || root == plan.Dir ||
		strings.HasPrefix(real["_main/tests/app/a.test.js"], root) {
		t.Fatalf("dir = %q, want a package under a staged root of its own", plan.Dir)
	}

	last := plan.Argv[len(plan.Argv)-1]
	if !strings.HasSuffix(last, "config.mjs") {
		t.Errorf("argv = %q, want it to end at the flags", plan.Argv)
	}
	want := []string{
		filepath.Join(root, "_main/tests/app/a.test.js"),
		filepath.Join(root, "_main/tests/app/b.test.js"),
		filepath.Join(root, "_main/tests/app/c.test.js"),
	}
	for i, link := range want {
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		rlocation := []string{"a", "b", "c"}[i]
		if expected, _ := filepath.EvalSymlinks(real["_main/tests/app/"+rlocation+".test.js"]); resolved != expected {
			t.Errorf("%s resolves to %q, want %q", link, resolved, expected)
		}
	}

	files := []string{}
	walk := func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	wantStaged := []string{
		"_main/node_modules",
		"_main/tests/app/_app_vitest.config.mjs",
		"_main/tests/app/a.test.js",
		"_main/tests/app/b.test.js",
		"_main/tests/app/c.test.js",
		"_main/tests/app/node_modules/vitest/vitest.mjs",
	}
	if strings.Join(files, ",") != strings.Join(wantStaged, ",") {
		t.Errorf("staged root holds %q, want exactly %q", files, wantStaged)
	}

	plan.Cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s behind", root)
	}
}

// vitest walks the root the launcher staged, the program's compiled files at
// their runfiles paths and nothing else, while vite's root stays in the tree.
func TestPlanVitestStagesTheFilesVitestWalksBesideTheTree(t *testing.T) {
	_, real := vitestFixture(t)
	r := runfilesTree(t, real)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	filesRoot := plan.EnvOverrides["TS_TEST_FILES_ROOT"]
	if filesRoot == "" || strings.HasPrefix(filesRoot, r.Dir()) {
		t.Fatalf("TS_TEST_FILES_ROOT = %q, want a root outside the tree %q",
			filesRoot, r.Dir())
	}
	if want := filepath.Join(r.Dir(), "_main/tests/app"); plan.Dir != want {
		t.Errorf("dir = %q, want vite's root in the tree, %q", plan.Dir, want)
	}
	staged := []string{}
	walk := func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(filesRoot, p)
			staged = append(staged, filepath.ToSlash(rel))
		}
		return nil
	}
	if err := filepath.WalkDir(filesRoot, walk); err != nil {
		t.Fatal(err)
	}
	slices.Sort(staged)
	want := []string{
		"_main/tests/app/a.test.js",
		"_main/tests/app/b.test.js",
		"_main/tests/app/c.test.js",
	}
	if strings.Join(staged, ",") != strings.Join(want, ",") {
		t.Errorf("the staged root holds %q, want exactly %q", staged, want)
	}
	for _, rel := range want {
		got, err := filepath.EvalSymlinks(filepath.Join(filesRoot, rel))
		expected, _ := filepath.EvalSymlinks(real[rel])
		if err != nil || got != expected {
			t.Errorf("%s resolves to %q, %v; want %q", rel, got, err, expected)
		}
	}
	if plan.Cleanup == nil {
		t.Fatal("the staged root has to come back off")
	}
	plan.Cleanup()
	if _, err := os.Stat(filesRoot); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s behind", filesRoot)
	}
}

// A test outside its importer's package meets no node_modules on the walk up
// before the workspace's root, so the importer's is linked in there.
func TestPlanVitestLinksTheImporterAtTheWorkspaceRoot(t *testing.T) {
	const importer = "_main/tests/npm/node_modules"
	_, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/_app.vitest/config.mjs": "export default {}",
		"_main/tests/app/app_test_files.txt":     "_main/tests/app/a.test.js",
		"_main/tests/app/a.test.js":              "x",
		importer:                                 dirMarker,
		importer + "/vitest/vitest.mjs":          "x",
		"+node+/bin/node":                        "#!/bin/sh\n",
	})
	r, root := withRunfilesDir(t, real[importer], importer)
	cfg := vitestConfig()
	cfg.Vitest.NodeModules = []string{importer}
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(root, "_main", "node_modules"))
	if err != nil {
		t.Fatalf("no node_modules link at the workspace root: %v", err)
	}
	if want := filepath.Join(root, filepath.FromSlash(importer)); got != want {
		t.Errorf("link -> %q, want the importer's node_modules %q", got, want)
	}
	vitest := filepath.Join(root, importer, "vitest/vitest.mjs")
	if !slices.Contains(plan.Argv, vitest) {
		t.Errorf("argv = %q, want vitest from the importer %q", plan.Argv, vitest)
	}
}

// vitest may be an importer above's: the chain is walked nearest first, as
// node walks up, and NODE_PATH names it in that order.
func TestPlanVitestWalksTheChainForVitest(t *testing.T) {
	_, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/_app.vitest/config.mjs":     "export default {}",
		"_main/tests/app/app_test_files.txt":         "_main/tests/app/a.test.js",
		"_main/tests/app/a.test.js":                  "x",
		"_main/tests/app/node_modules/zod/i.js":      "x",
		"_main/tests/node_modules/vitest/vitest.mjs": "x",
		"+node+/bin/node":                            "#!/bin/sh\n",
	})
	const test = "_main/tests/app/a.test.js"
	r, root := withRunfilesDir(t, real[test], test)
	cfg := vitestConfig()
	near, above := "_main/tests/app/node_modules", "_main/tests/node_modules"
	cfg.Vitest.NodeModules = []string{near, above}
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	vitest := filepath.Join(root, above, "vitest/vitest.mjs")
	if !slices.Contains(plan.Argv, vitest) {
		t.Errorf("argv = %q, want vitest from the importer above %q",
			plan.Argv, vitest)
	}
	want := filepath.Join(root, near) + string(os.PathListSeparator) +
		filepath.Join(root, above)
	if got := plan.EnvOverrides["NODE_PATH"]; got != want {
		t.Errorf("NODE_PATH = %q, want the chain nearest first %q", got, want)
	}
	link := filepath.Join(root, "_main", "node_modules")
	if got, _ := os.Readlink(link); got != filepath.Join(root, above) {
		t.Errorf("%s -> %q, want the chain's root", link, got)
	}
}

// A manifest-only layout has no directory to walk: the store is staged into
// the test root, a declared link keeping its relative target.
func TestPlanVitestStagesTheStoreFromTheManifest(t *testing.T) {
	base := filepath.Join(t.TempDir(), "var")
	if err := os.Symlink(t.TempDir(), base); err != nil {
		t.Fatal(err)
	}
	const store = "bin/tests/app/node_modules/.pnpm/vitest@4/node_modules/vitest"
	tree := filepath.Join(base, store)
	for rel, body := range map[string]string{
		"files/app_test_files.txt": "_main/tests/app/a.test.js\n",
		"files/a.test.js":          "x",
		"files/config.mjs":         "export default {}",
		"files/node":               "#!/bin/sh\n",
		store + "/vitest.mjs":      "x",
	} {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := filepath.Join(base, "files")
	r := manifestResolver(t, []string{
		"_main/tests/app/app_test_files.txt " + files + "/app_test_files.txt",
		"_main/tests/app/a.test.js " + files + "/a.test.js",
		"_main/tests/app/_app.vitest/config.mjs " + files + "/config.mjs",
		"_main/tests/app/node_modules/vitest .pnpm/vitest@4/node_modules/vitest",
		"_main/tests/app/node_modules/.pnpm/vitest@4/node_modules/vitest " + tree,
		"+node+/bin/node " + files + "/node",
	})
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	root := treeRoot(plan)
	link := filepath.Join(root, "_main/tests/app/node_modules/vitest")
	relative := filepath.FromSlash(".pnpm/vitest@4/node_modules/vitest")
	if got, err := os.Readlink(link); err != nil || got != relative {
		t.Errorf("readlink %s = %q, %v; want the manifest's target verbatim",
			link, got, err)
	}
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(tree)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("%s resolves to %q, want the store tree %q", link, got, want)
	}
	entry := filepath.Join(link, "vitest.mjs")
	if !slices.Contains(plan.Argv, entry) {
		t.Errorf("argv = %q, want vitest through the staged link %q",
			plan.Argv, entry)
	}
	nodeModules := filepath.Join(root, "_main/tests/app/node_modules")
	if got := plan.EnvOverrides["NODE_PATH"]; got != nodeModules {
		t.Errorf("NODE_PATH = %q, want the staged importer directory", got)
	}
}

// manifestResolver is a manifest-only layout over the given manifest lines.
func manifestResolver(t *testing.T, lines []string) *Resolver {
	t.Helper()
	manifest := filepath.Join(t.TempDir(), "MANIFEST")
	data := []byte(strings.Join(lines, "\n") + "\n")
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNFILES_DIR", "")
	t.Setenv("TEST_SRCDIR", "")
	t.Setenv("RUNFILES_MANIFEST_FILE", manifest)
	r, err := newResolver(runfiles.ManifestFile(manifest))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestManifestEntryReadsBothLineShapes(t *testing.T) {
	const link, tree = "_main/node_modules/zod", "../.pnpm/zod@3/node_modules/zod"
	const store = "_main/node_modules/.pnpm/zod@3/node_modules/zod"
	for _, tc := range []struct{ line, rlocation, target string }{
		{link + " " + tree, link, tree},
		{store + " /abs/tree", store, "/abs/tree"},
		{` _main/a\sb /abs/a\sb\bc`, "_main/a b", `/abs/a b\c`},
	} {
		rlocation, target, ok := manifestEntry(tc.line)
		if !ok || rlocation != tc.rlocation || target != tc.target {
			t.Errorf("manifestEntry(%q) = %q, %q, %v; want %q, %q",
				tc.line, rlocation, target, ok, tc.rlocation, tc.target)
		}
	}
	if _, _, ok := manifestEntry(""); ok {
		t.Error("an empty line is no entry")
	}
}

// withRunfilesDir rebuilds the resolver with RUNFILES_DIR at the fixture's tree
// root: fakeRunfiles clears it, and a resolver reads it once, at construction.
func withRunfilesDir(t *testing.T, real, rlocation string) (*Resolver, string) {
	t.Helper()
	root := strings.TrimSuffix(filepath.ToSlash(real), "/"+rlocation)
	t.Setenv("RUNFILES_DIR", root)
	r, err := directoryResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	return r, root
}

func TestEnvironCollapsesDuplicateKeys(t *testing.T) {
	t.Setenv("RUNFILES_DIR", "relative/path")
	t.Setenv("KEEP_ME", "yes")
	got := Environ(map[string]string{"RUNFILES_DIR": "/absolute/path"}, []string{"RUNFILES_DIR=from/library"})

	seen := 0
	for _, entry := range got {
		if strings.HasPrefix(entry, "RUNFILES_DIR=") {
			seen++
			if entry != "RUNFILES_DIR=/absolute/path" {
				t.Errorf("RUNFILES_DIR = %q, want the rule's value to win", entry)
			}
		}
	}
	if seen != 1 {
		t.Errorf("RUNFILES_DIR appears %d times; getenv would answer with the first", seen)
	}
	if !slices.Contains(got, "KEEP_ME=yes") {
		t.Error("Environ dropped an inherited variable")
	}
}

// The launcher links the importer's node_modules in at the workspace root;
// these pin what it does with whatever already sits on that name.

func TestPlanDevServerLinksTheNpmTreeIntoTheWorkspace(t *testing.T) {
	r, real := devServerFixture(t)
	ws := devServerWorkspace(t)
	plan, err := MakePlan(devServerConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "node_modules")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("no node_modules link at the workspace root: %v", err)
	}
	if target != real["_main/tests/app/node_modules"] {
		t.Errorf("link -> %q, want the node_modules %q", target,
			real["_main/tests/app/node_modules"])
	}
	if plan.Cleanup == nil {
		t.Fatal("a link the launcher made has to come back off")
	}
	plan.Cleanup()
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s behind (%v)", link, err)
	}
}

func TestPlanDevServerKeepsAnIdenticalLinkAndDoesNotRemoveIt(t *testing.T) {
	r, real := devServerFixture(t)
	ws := devServerWorkspace(t)
	link := filepath.Join(ws, "node_modules")
	if err := os.Symlink(real["_main/tests/app/node_modules"], link); err != nil {
		t.Fatal(err)
	}
	plan, err := MakePlan(devServerConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Another dev server on the same directory may own it; removing it breaks a
	// server this process never started.
	if plan.Cleanup != nil {
		plan.Cleanup()
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("a link it did not create was removed anyway: %v", err)
	}
}

func TestPlanDevServerRefusesALinkToAnotherNodeModules(t *testing.T) {
	r, _ := devServerFixture(t)
	ws := devServerWorkspace(t)
	other := t.TempDir()
	link := filepath.Join(ws, "node_modules")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	_, err := MakePlan(devServerConfig(), r, nil, Shard{Total: 1})
	if err == nil {
		t.Fatal("two node_modules cannot both be at the workspace root")
	}
	if !strings.Contains(err.Error(), other) {
		t.Errorf("the error does not name the directory already there: %v", err)
	}
	if target, _ := os.Readlink(link); target != other {
		t.Errorf("the existing link was replaced with %q", target)
	}
}

func TestPlanDevServerRefusesToDeleteAnInstalledNodeModules(t *testing.T) {
	r, _ := devServerFixture(t)
	ws := devServerWorkspace(t)
	link := filepath.Join(ws, "node_modules")
	if err := os.MkdirAll(filepath.Join(link, "left-behind"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := MakePlan(devServerConfig(), r, nil, Shard{Total: 1})
	if err == nil {
		t.Fatal("a real node_modules directory must not be taken over silently")
	}
	if _, statErr := os.Stat(filepath.Join(link, "left-behind")); statErr != nil {
		t.Errorf("the existing install was disturbed: %v", statErr)
	}
}

func TestPlanDevServerSubstitutesThePortIntoArgvWithoutRepeatingIt(t *testing.T) {
	r, real := devServerFixture(t)
	devServerWorkspace(t)
	cfg := devServerConfig()
	cfg.DevServer.Argv = []string{"dev", "--config", "{config}", "--port", "{port}", "{root}"}

	plan, err := MakePlan(cfg, r, []string{"--port", "4321"}, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.Join(plan.Argv, " "), "--port"); got != 1 {
		t.Errorf("argv names --port %d times, want once: %q", got, plan.Argv)
	}
	// And the override is what it was given, not the port the rule configured.
	if !slices.Contains(plan.Argv, "4321") || slices.Contains(plan.Argv, "5173") {
		t.Errorf("argv = %q, want the overriding port 4321", plan.Argv)
	}
	_ = real
}

// A dev server told where to put its scratch has to find the directory there:
// a tool handed a path that does not exist may or may not create one.
func TestPlanDevServerCreatesTheScratchDirectoriesItNames(t *testing.T) {
	r, _ := devServerFixture(t)
	ws := devServerWorkspace(t)
	cfg := devServerConfig()
	cfg.DevServer.ScratchDir = "tests/app/dev_dev"

	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TSR_TMP_DIR", "OJ_CACHE_DIR"} {
		dir := plan.EnvOverrides[name]
		if dir == "" {
			t.Errorf("%s is unset", name)
			continue
		}
		if !strings.HasPrefix(dir, filepath.Join(ws, "bazel-bin")) {
			t.Errorf("%s = %q, want it under bazel-bin, not in the source tree", name, dir)
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Errorf("%s names %q, which is not a directory (%v)", name, dir, err)
		}
	}
}

// pnpm runs vitest from the package, and a test's relative read or a config's
// __dirname names files there: the run's directory is the config's package.
func TestPlanVitestRunsFromTheConfigsPackage(t *testing.T) {
	_, real := vitestFixture(t)
	r := runfilesTree(t, real)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.Dir(), "_main/tests/app"); plan.Dir != want {
		t.Errorf("dir = %q, want the config's package %q", plan.Dir, want)
	}
}

func TestPlanVitestRunsFromAnAncestorConfigsPackage(t *testing.T) {
	_, real := vitestFixture(t)
	r := runfilesTree(t, real)
	cfg := vitestConfig()
	cfg.Vitest.RootRel = ".."
	plan, err := MakePlan(cfg, r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.Dir(), "_main/tests"); plan.Dir != want {
		t.Errorf("dir = %q, want the ancestor package %q", plan.Dir, want)
	}
}

// Vite bundles a config from its realpath, which for a runfiles symlink is
// bazel-out: the config vitest is handed is a regular file in the package.
func TestPlanVitestWritesTheConfigAsARegularFileInThePackage(t *testing.T) {
	_, real := vitestFixture(t)
	r := runfilesTree(t, real)
	plan, err := MakePlan(vitestConfig(), r, nil, Shard{Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(r.Dir(), "_main/tests/app/_app_vitest.config.mjs")
	st, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("no config at the package path: %v", err)
	}
	if !st.Mode().IsRegular() {
		t.Errorf("%s is %v, want a regular file", dst, st.Mode())
	}
	if got, _ := os.ReadFile(dst); string(got) != "export default {}" {
		t.Errorf("content = %q, want the generated config's", got)
	}
	joined := strings.Join(plan.Argv, " ")
	if !strings.Contains(joined, "--config "+dst) {
		t.Errorf("argv %q does not hand vitest %q", joined, dst)
	}
}

// A config that is also a package src has a runfiles symlink at its own path;
// the copy replaces it, and is written from its private entry every run.
func TestPlanVitestReplacesASymlinkAtAStagedPath(t *testing.T) {
	const from = "_main/tests/app/_app.vitest/tests/app/vitest.config.mts"
	const to = "_main/tests/app/vitest.config.mts"
	const fresh = "export default { fresh: true }"
	_, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/_app.vitest/config.mjs":         "export default {}",
		from:                                             fresh,
		"_main/tests/app/app_test_files.txt":             "_main/tests/app/a.test.js",
		"_main/tests/app/a.test.js":                      "x",
		"_main/tests/app/node_modules":                   dirMarker,
		"_main/tests/app/node_modules/vitest/vitest.mjs": "x",
		"+node+/bin/node":                                "#!/bin/sh\n",
	})
	r := runfilesTree(t, real)
	stale := filepath.Join(t.TempDir(), "stale.mts")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(r.Dir(), filepath.FromSlash(to))
	if err := os.Symlink(stale, dst); err != nil {
		t.Fatal(err)
	}
	cfg := vitestConfig()
	cfg.Vitest.Stage[from] = to
	if _, err := MakePlan(cfg, r, nil, Shard{Total: 1}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Mode().IsRegular() {
		t.Errorf("%s is %v, want a regular file over the symlink", dst, st.Mode())
	}
	if got, _ := os.ReadFile(dst); string(got) != fresh {
		t.Errorf("content = %q, want the private entry's", got)
	}
	if got, _ := os.ReadFile(stale); string(got) != "stale" {
		t.Errorf("the symlink's target was written through: %q", got)
	}
}

func TestAdvertiseSharding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status")
	if err := advertiseSharding(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := advertiseSharding(""); err != nil {
		t.Fatal(err)
	}
	if err := advertiseSharding(filepath.Join(path, "missing")); err == nil {
		t.Fatal("failed handshake must not be ignored")
	}
}

func TestStageTestRootRemovesPartialRoot(t *testing.T) {
	for _, failure := range []string{"directory", "symlink"} {
		t.Run(failure, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "test.js")
			if err := os.WriteFile(source, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			scratch := t.TempDir()
			t.Setenv("TEST_TMPDIR", scratch)
			next := "test.js/child.js"
			if failure == "symlink" {
				next = "invalid\x00"
			}
			if _, err := stageTestRoot([]testFile{{"test.js", source}, {next, source}}); err == nil {
				t.Fatal("conflicting stage path must fail")
			}
			if files, err := os.ReadDir(scratch); err != nil || len(files) != 0 {
				t.Fatalf("staging left files behind: %v, %v", files, err)
			}
		})
	}
}

func TestPlanVitestRemovesRootOnFailure(t *testing.T) {
	r, _ := vitestFixture(t)
	cfg := vitestConfig()
	cfg.Vitest.VitestInTree = "vitest/missing.mjs"
	scratch := t.TempDir()
	t.Setenv("TEST_TMPDIR", scratch)
	if _, err := MakePlan(cfg, r, nil, Shard{Total: 1}); err == nil {
		t.Fatal("missing Vitest must fail")
	}
	if files, err := os.ReadDir(scratch); err != nil || len(files) != 0 {
		t.Fatalf("failed plan left files behind: %v, %v", files, err)
	}
}
