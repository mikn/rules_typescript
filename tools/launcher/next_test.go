package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nextFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	return fakeRunfiles(t, map[string]string{
		"+node+/bin/node":                                "#!/bin/sh\n",
		"_main/apps/web/node_modules":                    dirMarker,
		"_main/apps/web/node_modules/next/dist/bin/next": "#!/usr/bin/env node\n",
		"_main/apps/web/next.config.mjs":                 "export default {};\n",
		"_main/apps/web/public/logo.png":                 "png",
		"_main/apps/web/app_next_out":                    dirMarker,
		"_main/apps/web/app_next_out/BUILD_ID":           "abc",
	})
}

func nextDevConfig() *Config {
	return &Config{
		Label:   "//apps/web:dev",
		Mode:    ModeNext,
		Runtime: "+node+/bin/node",
		Next: &NextConfig{
			Command:     nextCommandDev,
			NodeModules: "_main/apps/web/node_modules",
			ProjectDir:  "apps/web",
			Port:        3000,
		},
	}
}

func nextServeConfig() *Config {
	return &Config{
		Label:   "//apps/web:serve",
		Mode:    ModeNext,
		Runtime: "+node+/bin/node",
		Next: &NextConfig{
			Command:       nextCommandStart,
			NodeModules:   "_main/apps/web/node_modules",
			BuildDir:      "_main/apps/web/app_next_out",
			ConfigFile:    "_main/apps/web/next.config.mjs",
			ProjectFiles:  []string{"_main/apps/web/public/logo.png"},
			PackagePrefix: "_main/apps/web/",
			Port:          3000,
		},
	}
}

// `next dev` serves source, so the project directory is the package under the
// workspace -- not a staging directory, and not the runfiles tree.
func TestPlanNextDevServesTheSourceTree(t *testing.T) {
	r, real := nextFixture(t)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "apps", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", workspace)

	plan, err := MakePlan(nextDevConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := real["_main/apps/web/node_modules"]
	want := []string{
		real["+node+/bin/node"],
		filepath.Join(tree, "next", "dist", "bin", "next"),
		"dev", "--port", "3000",
	}
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
	if plan.Dir != filepath.Join(workspace, "apps", "web") {
		t.Errorf("dir = %q, want the package inside the workspace", plan.Dir)
	}
	// Next.js seeds webpack's resolve.modules from NODE_PATH; that is the whole
	// reason no node_modules symlink is planted in the source tree.
	if !strings.HasPrefix(plan.EnvOverrides["NODE_PATH"], tree) {
		t.Errorf("NODE_PATH = %q, want the npm tree %q first", plan.EnvOverrides["NODE_PATH"], tree)
	}
	if !plan.Supervise.IgnoreTerm {
		t.Error("ibazel SIGTERMs the runner on rebuild; the server must survive it")
	}
}

func TestPlanNextDevExplainsAMissingNext(t *testing.T) {
	r, _ := fakeRunfiles(t, map[string]string{
		"+node+/bin/node":             "#!/bin/sh\n",
		"_main/apps/web/node_modules": dirMarker,
	})
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", t.TempDir())
	_, err := MakePlan(nextDevConfig(), r, nil)
	if err == nil {
		t.Fatal("a node_modules tree without next must fail")
	}
	if !strings.Contains(err.Error(), "@npm//:next") {
		t.Errorf("error does not say what to add: %v", err)
	}
}

// A test needs a kernel-assigned port. The launcher consumes the override
// instead of appending it, so its own message and the listening socket cannot
// disagree.
func TestPlanNextPortArgumentReplacesTheConfiguredPort(t *testing.T) {
	for _, args := range [][]string{
		{"--port", "4321"},
		{"-p", "4321"},
		{"--port=4321"},
	} {
		r, _ := nextFixture(t)
		workspace := t.TempDir()
		if err := os.MkdirAll(filepath.Join(workspace, "apps", "web"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BUILD_WORKSPACE_DIRECTORY", workspace)

		plan, err := MakePlan(nextDevConfig(), r, args)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(plan.Argv, " ")
		if !strings.HasSuffix(joined, "dev --port 4321") {
			t.Errorf("argv for %q ends %q, want a single --port 4321", args, joined)
		}
	}
}

// The image optimizer writes into .next/cache at request time, so the build
// output has to be a writable copy rather than the read-only Bazel tree.
func TestPlanNextServeStagesAWritableProjectDirectory(t *testing.T) {
	r, _ := nextFixture(t)
	t.Setenv("TEST_TMPDIR", t.TempDir())

	plan, err := MakePlan(nextServeConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Cleanup == nil {
		t.Error("the staged directory is never removed")
	}
	buildID := filepath.Join(plan.Dir, ".next", "BUILD_ID")
	info, err := os.Stat(buildID)
	if err != nil {
		t.Fatalf("the build output was not staged as .next: %v", err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Errorf("%s is not writable (mode %v); next start writes its image cache under .next", buildID, info.Mode())
	}
	for _, rel := range []string{"next.config.mjs", "public/logo.png", "package.json"} {
		if _, err := os.Stat(filepath.Join(plan.Dir, rel)); err != nil {
			t.Errorf("%s is not in the staged project directory: %v", rel, err)
		}
	}
	if !strings.HasSuffix(strings.Join(plan.Argv, " "), "start --port 3000") {
		t.Errorf("argv = %q, want `next start --port 3000`", plan.Argv)
	}
	plan.Cleanup()
	if _, err := os.Stat(plan.Dir); err == nil {
		t.Errorf("%s survived cleanup", plan.Dir)
	}
}

func TestParseConfigRejectsAnUnknownNextCommand(t *testing.T) {
	_, err := ParseConfig([]byte(`{"mode":"next","next":{"command":"deploy","node_modules":"_main/nm"}}`))
	if err == nil || !strings.Contains(err.Error(), "next.command") {
		t.Fatalf("want a rejection naming next.command, got %v", err)
	}
}

func TestParseConfigRequiresABuildForNextStart(t *testing.T) {
	_, err := ParseConfig([]byte(`{"mode":"next","next":{"command":"start","node_modules":"_main/nm"}}`))
	if err == nil || !strings.Contains(err.Error(), "build_dir") {
		t.Fatalf("want a rejection naming build_dir, got %v", err)
	}
}
