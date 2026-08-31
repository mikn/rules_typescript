package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	plan, err := MakePlan(cfg, r, []string{"--flag", "a b"})
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
	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Argv[0] != "node" {
		t.Errorf("argv[0] = %q, want the system node fallback", plan.Argv[0])
	}
}

func TestPlanNodeAddsNodeModulesToNodePath(t *testing.T) {
	r, real := fakeRunfiles(t, map[string]string{
		"_main/a.js":               "x",
		"_main/tests/node_modules": dirMarker,
	})
	t.Setenv("NODE_PATH", "/pre-existing")
	cfg := &Config{Mode: ModeNode, Node: &NodeConfig{
		Entry:       "_main/a.js",
		NodeModules: "_main/tests/node_modules",
	}}
	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := real["_main/tests/node_modules"] + string(os.PathListSeparator) + "/pre-existing"
	if plan.EnvOverrides["NODE_PATH"] != want {
		t.Errorf("NODE_PATH = %q, want %q", plan.EnvOverrides["NODE_PATH"], want)
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
	plan, err := MakePlan(cfg, r, nil)
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
		"_main/tests/app/_app_vitest.config.mjs": "export default {}",
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
			NodeModules:   "_main/tests/app/node_modules",
		},
	}
}

func TestPlanVitestRunsEveryTestFileByDefault(t *testing.T) {
	r, real := vitestFixture(t)
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	joined := strings.Join(plan.Argv, " ")
	for _, want := range []string{
		real["+node+/bin/node"],
		filepath.Join(real["_main/tests/app/node_modules"], "vitest", "vitest.mjs"),
		"run --config " + real["_main/tests/app/_app_vitest.config.mjs"],
		filepath.Join(plan.Dir, "_main/tests/app/a.test.js"),
		filepath.Join(plan.Dir, "_main/tests/app/c.test.js"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q is missing %q", joined, want)
		}
	}
	if plan.UseExec {
		t.Error("the test runner has to outlive vitest to post-process coverage")
	}
}

func TestPlanVitestPartitionsShards(t *testing.T) {
	r, _ := vitestFixture(t)
	t.Setenv("TEST_TOTAL_SHARDS", "2")
	t.Setenv("TEST_SHARD_INDEX", "1")
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	joined := strings.Join(plan.Argv, " ")
	if !strings.Contains(joined, filepath.Join(plan.Dir, "_main/tests/app/b.test.js")) {
		t.Errorf("shard 1 of 2 should run b.test.js, got %q", joined)
	}
	if strings.Contains(joined, "a.test.js") {
		t.Errorf("shard 1 of 2 should not run a.test.js, got %q", joined)
	}
	if _, err := os.Lstat(filepath.Join(plan.Dir, "_main/tests/app/a.test.js")); !os.IsNotExist(err) {
		t.Error("a.test.js belongs to the other shard; staging it would let vitest glob it")
	}
}

func TestPlanVitestExitsCleanlyOnAnEmptyShard(t *testing.T) {
	r, _ := vitestFixture(t)
	t.Setenv("TEST_TOTAL_SHARDS", "8")
	t.Setenv("TEST_SHARD_INDEX", "7")
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ExitEarly {
		t.Fatal("an empty shard must not start vitest")
	}
}

func TestPlanVitestSkipsTheRuntimeForAnNpmBinWrapper(t *testing.T) {
	r, real := vitestFixture(t)
	cfg := vitestConfig()
	cfg.Vitest.Vitest = "_main/tests/app/node_modules/vitest/vitest.mjs"
	cfg.Vitest.VitestIsNpmBin = true
	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	if plan.Argv[0] == real["+node+/bin/node"] {
		t.Errorf("an npm_bin wrapper resolves its own node; argv = %q", plan.Argv)
	}
}

func TestPlanVitestAddsCoverageFlagsUnderBazelCoverage(t *testing.T) {
	r, _ := vitestFixture(t)
	out := filepath.Join(t.TempDir(), "coverage", "out.dat")
	t.Setenv("COVERAGE_OUTPUT_FILE", out)
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	joined := strings.Join(plan.Argv, " ")
	if !strings.Contains(joined, "--coverage.reportsDirectory "+filepath.Dir(out)) {
		t.Errorf("argv %q is missing the lcov output directory", joined)
	}
	if plan.PostRun == nil {
		t.Fatal("coverage runs need the lcov post-processing step")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(out), "lcov.info"),
		[]byte("SF:_main/tests/app/a.js\nDA:1,1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plan.PostRun(0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "SF:tests/app/a.js") {
		t.Errorf("lcov = %q, want the repository prefix stripped", got)
	}
}

func TestPlanVitestWritesEmptyCoverageWhenVitestProducedNone(t *testing.T) {
	r, _ := vitestFixture(t)
	out := filepath.Join(t.TempDir(), "coverage", "out.dat")
	t.Setenv("COVERAGE_OUTPUT_FILE", out)
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	if err := plan.PostRun(1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("bazel coverage requires the output file to exist: %v", err)
	}
}

func TestPlanVitestEnablesCoverageFromTheAttrOnAPlainTestRun(t *testing.T) {
	r, _ := vitestFixture(t)
	tmp := t.TempDir()
	t.Setenv("TEST_TMPDIR", tmp)
	t.Setenv("COVERAGE_OUTPUT_FILE", "")
	cfg := vitestConfig()
	cfg.Vitest.Coverage = true
	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	joined := strings.Join(plan.Argv, " ")
	if !strings.Contains(joined, "--coverage.enabled true") {
		t.Errorf("coverage = True has to enable coverage under `bazel test`; argv = %q", joined)
	}
	if !strings.Contains(joined, "--coverage.reportsDirectory "+filepath.Join(tmp, "coverage")) {
		t.Errorf("a report written into the runfiles tree would be a write to a test input; argv = %q", joined)
	}
}

func TestPlanVitestKeepsOnlyTheFilesBazelInstrumented(t *testing.T) {
	r, _ := vitestFixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "coverage.dat")
	manifest := filepath.Join(dir, "instrumented.txt")
	if err := os.WriteFile(manifest, []byte(
		"tests/app/kept.ts\nbazel-out/k8-fastbuild/bin/tests/app/generated.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COVERAGE_OUTPUT_FILE", out)
	t.Setenv("COVERAGE_MANIFEST", manifest)
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	if err := os.WriteFile(filepath.Join(dir, "lcov.info"), []byte(
		"TN:\nSF:_main/tests/app/kept.js\nDA:1,1\nend_of_record\n"+
			"TN:\nSF:_main/tests/app/generated.js\nDA:1,1\nend_of_record\n"+
			"TN:\nSF:_main/tests/app/filtered.js\nDA:1,0\nend_of_record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plan.PostRun(0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "TN:\nSF:tests/app/kept.js\nDA:1,1\nend_of_record\n" +
		"TN:\nSF:tests/app/generated.js\nDA:1,1\nend_of_record\n"
	if string(got) != want {
		t.Errorf("lcov = %q, want %q", got, want)
	}
}

func TestSelectInstrumentedTreatsAnEmptySelectionAsExcludingEverything(t *testing.T) {
	in := "SF:tests/app/a.js\nDA:1,1\nend_of_record\n"
	if got := string(SelectInstrumented([]byte(in), nil)); got != "" {
		t.Errorf("SelectInstrumented = %q, want an empty report", got)
	}
}

func TestCoverageKeyMatchesASourceAgainstWhatWasCompiledFromIt(t *testing.T) {
	cases := map[string]string{
		"tests/app/a.ts": "tests/app/a",
		"bazel-out/k8-fastbuild/bin/tests/app/a.js": "tests/app/a",
		"  tests/app/a.tsx  ":                       "tests/app/a",
		"":                                          "",
	}
	for in, want := range cases {
		if got := coverageKey(in); got != want {
			t.Errorf("coverageKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteLcovOnlyTouchesSourceFileLines(t *testing.T) {
	in := []byte("SF:_main/a.js\nFN:1,_main/x\nSF:_mainless/b.js\n")
	got := string(RewriteLcov(in, "_main", ""))
	want := "SF:a.js\nFN:1,_main/x\nSF:_mainless/b.js\n"
	if got != want {
		t.Errorf("RewriteLcov = %q, want %q", got, want)
	}
}

func TestRewriteLcovResolvesBuildOutputsReportedFromOutsideTheRoot(t *testing.T) {
	runDir := "/w/execroot/_main/bazel-out/k8-fastbuild/bin/tests/workers/t.runfiles"
	cases := []struct {
		name string
		in   string
		want string
	}{{
		name: "escaping relative, as istanbul writes an out-of-root module",
		in:   "SF:../../../../../execroot/_main/bazel-out/k8-fastbuild/bin/tests/workers/src/index.js\n",
		want: "SF:tests/workers/src/index.js\n",
	}, {
		name: "absolute",
		in:   "SF:/w/execroot/_main/bazel-out/k8-fastbuild/bin/tests/workers/src/index.js\n",
		want: "SF:tests/workers/src/index.js\n",
	}, {
		name: "a path with no bazel-out in it is left alone",
		in:   "SF:/home/me/src/tests/workers/src/index.js\n",
		want: "SF:/home/me/src/tests/workers/src/index.js\n",
	}, {
		name: "a relative path staying inside the runfiles tree is left alone",
		in:   "SF:_other/x.js\n",
		want: "SF:_other/x.js\n",
	}, {
		name: "a runfiles path keeps the repository-prefix rule",
		in:   "SF:_main/tests/vitest/math.js\n",
		want: "SF:tests/vitest/math.js\n",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(RewriteLcov([]byte(tc.in), "_main", runDir)); got != tc.want {
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
		"_main/oj/oj":                                   "#!/bin/sh\n",
		"+node+/bin/node":                               "#!/bin/sh\n",
	})
}

// devServerWorkspace is a real directory, because planDevServer links the npm
// tree into it as node_modules.
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
	plan, err := MakePlan(devServerConfig(), r, []string{"--host"})
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
	if plan.Dir != ws {
		t.Errorf("dir = %q, want the workspace root", plan.Dir)
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
	_, err := MakePlan(cfg, r, nil)
	if err == nil {
		t.Fatal("a node_modules tree without vite must fail")
	}
	if !strings.Contains(err.Error(), "node_modules() target") {
		t.Errorf("error is not actionable: %v", err)
	}
}

func ojDevServerConfig() *Config {
	cfg := devServerConfig()
	cfg.DevServer.ServerInTree = ""
	cfg.DevServer.ServerBinary = "_main/oj/oj"
	cfg.DevServer.Argv = []string{"dev", "--config", "{config}", "{root}"}
	cfg.DevServer.RunsInJsRuntime = false
	return cfg
}

func TestPlanDevServerRunsANativeServerWithoutTheJsRuntime(t *testing.T) {
	r, real := devServerFixture(t)
	ws := devServerWorkspace(t)
	plan, err := MakePlan(ojDevServerConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		real["_main/oj/oj"], "dev", "--config",
		real["_main/tests/app/dev_vite.config.mjs"], ws, "--port", "5173",
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
	_, err := MakePlan(cfg, r, nil)
	if err == nil || !strings.Contains(err.Error(), "node_modules") {
		t.Fatalf("want an actionable node_modules error, got %v", err)
	}
}

// Without a runfiles tree the files live in bazel-bin, beside every sibling
// target's identically named copy, which vitest's root would glob in.
func TestPlanVitestStagesAPrivateRootWithoutARunfilesDirectory(t *testing.T) {
	r, real := vitestFixture(t)
	plan, err := MakePlan(vitestConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Dir == "" || plan.Dir == filepath.Dir(real["_main/tests/app/a.test.js"]) {
		t.Fatalf("dir = %q, want a staged root of this test's own", plan.Dir)
	}

	want := []string{
		filepath.Join(plan.Dir, "_main/tests/app/a.test.js"),
		filepath.Join(plan.Dir, "_main/tests/app/b.test.js"),
		filepath.Join(plan.Dir, "_main/tests/app/c.test.js"),
	}
	got := plan.Argv[len(plan.Argv)-len(want):]
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("positional args = %q, want %q", got, want)
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
	if err := filepath.WalkDir(plan.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(plan.Dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	wantStaged := []string{
		"_main/tests/app/a.test.js",
		"_main/tests/app/b.test.js",
		"_main/tests/app/c.test.js",
		"node_modules",
	}
	if strings.Join(files, ",") != strings.Join(wantStaged, ",") {
		t.Errorf("staged root holds %q, want exactly %q", files, wantStaged)
	}

	root := plan.Dir
	plan.Cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s behind", root)
	}
}

func TestEnvironCollapsesDuplicateKeys(t *testing.T) {
	t.Setenv("RUNFILES_DIR", "relative/path")
	t.Setenv("KEEP_ME", "yes")
	got := Environ(map[string]string{"RUNFILES_DIR": "/absolute/path"}, nil, []string{"RUNFILES_DIR=from/library"})

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

// Removing a variable is not setting it to the empty string: to getenv an empty
// value is still a variable that is set, and wrangler treats a present
// CLOUDFLARE_API_TOKEN as an attempt to authenticate.
func TestEnvironRemovesUnsetKeys(t *testing.T) {
	t.Setenv("DROP_ME", "value")
	t.Setenv("KEEP_ME", "value")
	got := Environ(nil, []string{"DROP_ME", "ALSO_DROP_ME"}, []string{"ALSO_DROP_ME=from/library"})
	for _, entry := range got {
		for _, key := range []string{"DROP_ME=", "ALSO_DROP_ME="} {
			if strings.HasPrefix(entry, key) {
				t.Errorf("%s survived, empty or not", strings.TrimSuffix(key, "="))
			}
		}
	}
	if !slices.Contains(got, "KEEP_ME=value") {
		t.Error("Environ dropped a variable it was not asked to")
	}
}

// The npm tree is a Bazel output with no node_modules above the source that
// imports from it, so the launcher links it in at the workspace root. These
// pin what it does with whatever is already sitting on that name -- getting it
// wrong serves a stale install, or deletes one.

func TestPlanDevServerLinksTheNpmTreeIntoTheWorkspace(t *testing.T) {
	r, real := devServerFixture(t)
	ws := devServerWorkspace(t)
	plan, err := MakePlan(devServerConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "node_modules")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("no node_modules link at the workspace root: %v", err)
	}
	if target != real["_main/tests/app/node_modules"] {
		t.Errorf("link -> %q, want the npm tree %q", target, real["_main/tests/app/node_modules"])
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
	plan, err := MakePlan(devServerConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Another dev server on the same tree may own it; removing it would break a
	// server this process never started.
	if plan.Cleanup != nil {
		plan.Cleanup()
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("a link it did not create was removed anyway: %v", err)
	}
}

func TestPlanDevServerRefusesALinkToAnotherTree(t *testing.T) {
	r, _ := devServerFixture(t)
	ws := devServerWorkspace(t)
	other := t.TempDir()
	link := filepath.Join(ws, "node_modules")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	_, err := MakePlan(devServerConfig(), r, nil)
	if err == nil {
		t.Fatal("two npm trees cannot both be at the workspace root")
	}
	if !strings.Contains(err.Error(), other) {
		t.Errorf("the error does not name the tree already there: %v", err)
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
	_, err := MakePlan(devServerConfig(), r, nil)
	if err == nil {
		t.Fatal("a real node_modules directory must not be taken over silently")
	}
	if _, statErr := os.Stat(filepath.Join(link, "left-behind")); statErr != nil {
		t.Errorf("the existing install was disturbed: %v", statErr)
	}
}

// A server that names the port in its own argv -- oj does, because its TanStack
// Start path never reads the config -- has to be given it once. Passing it twice
// is not a later-wins: oj exits with "cannot be used multiple times".
func TestPlanDevServerSubstitutesThePortIntoArgvWithoutRepeatingIt(t *testing.T) {
	r, real := devServerFixture(t)
	devServerWorkspace(t)
	cfg := devServerConfig()
	cfg.DevServer.Argv = []string{"dev", "--config", "{config}", "--port", "{port}", "{root}"}

	plan, err := MakePlan(cfg, r, []string{"--port", "4321"})
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

	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OJ_CACHE_DIR", "TSR_TMP_DIR"} {
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
