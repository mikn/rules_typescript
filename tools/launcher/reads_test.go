package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if content == dirMarker {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadsReportNamesTheNearestPackage(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"BUILD.bazel":   "",
		"a/BUILD.bazel": "",
		"a/b/c.txt":     "x",
		"a/d.txt":       "x",
		"e.txt":         "x",
		"f/BUILD":       "",
		"f/g.txt":       "x",
	})
	got := ReadsReport(ws, []string{"a/b/c.txt", "e.txt", "a/d.txt", "f/g.txt"})
	want := []string{
		"a/b/c.txt\t//a:b/c.txt",
		"a/d.txt\t//a:d.txt",
		"e.txt\t//:e.txt",
		"f/g.txt\t//f:g.txt",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestReadsReportIsSortedOnceAndFilesOnly(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"BUILD.bazel": "",
		"a/b/c.txt":   "x",
		"a/d.txt":     "x",
		"a/e":         dirMarker,
	})
	got := ReadsReport(ws, []string{
		"a/d.txt", "a/b/c.txt", "a/d.txt", "", "a/e", "a/b", "missing.txt",
	})
	want := []string{"a/b/c.txt\t//:a/b/c.txt", "a/d.txt\t//:a/d.txt"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestRunfilesBesideTheBinary(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"launcher":          "#!/bin/sh\n",
		"launcher.runfiles": dirMarker,
		"other":             "#!/bin/sh\n",
	})
	want := filepath.Join(dir, "launcher.runfiles")
	if got := runfilesBeside(filepath.Join(dir, "launcher")); got != want {
		t.Errorf("runfilesBeside(launcher) = %q, want %q", got, want)
	}
	if got := runfilesBeside(filepath.Join(dir, "other")); got != "" {
		t.Errorf("runfilesBeside(other) = %q, want none", got)
	}
	if got := runfilesBeside("launcher"); got != "" {
		t.Errorf("runfilesBeside(a bare name) = %q, want none", got)
	}
}

// readsFixture is vitestFixture's layout as a runfiles tree, so the resolver
// has a directory the way `bazel test` gives it one.
func readsFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	list := "_main/tests/app/a.test.js\n"
	_, real := fakeRunfiles(t, map[string]string{
		"_main/tests/app/_app_vitest.config.mjs":         "x",
		"_main/tests/app/app_test_files.txt":             list,
		"_main/tests/app/a.test.js":                      "x",
		"_main/tests/app/node_modules":                   dirMarker,
		"_main/tests/app/node_modules/vitest/vitest.mjs": "x",
		"_main/ts/private/reads_hook.cjs":                "x",
		"+node+/bin/node":                                "#!/bin/sh\n",
	})
	tree := strings.TrimSuffix(real["+node+/bin/node"], "/+node+/bin/node")
	t.Setenv("RUNFILES_MANIFEST_FILE", "")
	t.Setenv("RUNFILES_DIR", tree)
	r, err := newResolver(runfiles.Directory(tree))
	if err != nil {
		t.Fatal(err)
	}
	return r, real
}

func readsConfig() *Config {
	cfg := vitestConfig()
	cfg.Vitest.ReadsHook = "_main/ts/private/reads_hook.cjs"
	return cfg
}

func TestPlanVitestReadsInstallsTheHookBelowTheRunner(t *testing.T) {
	r, real := readsFixture(t)
	ws := t.TempDir()
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", ws)
	t.Setenv("NODE_OPTIONS", "--inspect")
	plan, err := MakePlan(readsConfig(), r, []string{ReadsFlag})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	env := plan.EnvOverrides
	hook := "--inspect --require " + real["_main/ts/private/reads_hook.cjs"]
	if env["NODE_OPTIONS"] != hook {
		t.Errorf("NODE_OPTIONS = %q, want %q", env["NODE_OPTIONS"], hook)
	}
	if root, _ := filepath.EvalSymlinks(ws); env["TS_TEST_READS_ROOT"] != root {
		t.Errorf("TS_TEST_READS_ROOT = %q, want %q", env["TS_TEST_READS_ROOT"], root)
	}
	if tree := env["TS_TEST_READS_RUNFILES"]; tree != r.Dir() {
		t.Errorf("TS_TEST_READS_RUNFILES = %q, want %q", tree, r.Dir())
	}
	if _, err := os.Stat(env["TS_TEST_READS_FILE"]); err != nil {
		t.Errorf("TS_TEST_READS_FILE %q: %v", env["TS_TEST_READS_FILE"], err)
	}
	if !plan.Supervise.StdoutToStderr {
		t.Error("vitest's stdout has to leave stdout to the report")
	}
	if plan.Dir != r.Dir() {
		t.Errorf("dir = %q, want the runfiles tree %q", plan.Dir, r.Dir())
	}
	if plan.PostRun == nil {
		t.Fatal("no PostRun: nothing would print the report")
	}
}

func TestReadsReportPrintsTheRecordedFiles(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"BUILD.bazel":          "",
		"fixtures/BUILD.bazel": "",
		"fixtures/f.txt":       "x",
	})
	record := filepath.Join(t.TempDir(), "record")
	twice := []byte("fixtures/f.txt\nfixtures/f.txt\n")
	if err := os.WriteFile(record, twice, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	x := &readsRun{workspace: ws, record: record}
	if err := x.report(&out)(0); err != nil {
		t.Fatal(err)
	}
	if want := "fixtures/f.txt\t//fixtures:f.txt\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	empty := &readsRun{workspace: ws, record: filepath.Join(t.TempDir(), "none")}
	out.Reset()
	if err := empty.report(&out)(0); err != nil || out.Len() != 0 {
		t.Errorf("an empty record printed %q, err %v", out.String(), err)
	}
}

func TestPlanVitestReadsNeedsBazelRun(t *testing.T) {
	r, _ := readsFixture(t)
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", "")
	_, err := MakePlan(readsConfig(), r, []string{ReadsFlag})
	if err == nil || !strings.Contains(err.Error(), "bazel run") {
		t.Errorf("err = %v, want one naming `bazel run`", err)
	}
}

// The hook is in every vitest test's config; only the flag installs it.
func TestPlanVitestLeavesTheReadsHookOutWithoutTheFlag(t *testing.T) {
	r, real := readsFixture(t)
	t.Setenv("NODE_OPTIONS", "")
	plan, err := MakePlan(readsConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	hook := real["_main/ts/private/reads_hook.cjs"]
	options := plan.EnvOverrides["NODE_OPTIONS"]
	if strings.Contains(options, hook) {
		t.Errorf("NODE_OPTIONS = %q, want no hook without %s", options,
			ReadsFlag)
	}
}

// The launcher consumes --reads; every other argument is vitest's.
func TestPlanVitestHandsTheOtherArgumentsToVitest(t *testing.T) {
	r, _ := readsFixture(t)
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", t.TempDir())
	args := []string{ReadsFlag, "--reporter=dot"}
	plan, err := MakePlan(readsConfig(), r, args)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Cleanup()
	if slices.Contains(plan.Argv, ReadsFlag) {
		t.Errorf("argv = %q, want %s consumed", plan.Argv, ReadsFlag)
	}
	if !slices.Contains(plan.Argv, "--reporter=dot") {
		t.Errorf("argv = %q, want --reporter=dot handed to vitest", plan.Argv)
	}
}
