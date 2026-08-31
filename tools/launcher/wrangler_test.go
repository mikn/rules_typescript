package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// wrangler compiles TypeScript itself, so a worker's config names the .ts entry
// -- that is what `wrangler dev` needs. Bazel stages what it compiled, so the
// only file beside the staged config is the .js, and wrangler stops at
// "The entry-point file at src/index.ts was not found."
func TestRetargetMain(t *testing.T) {
	staged := map[string]bool{"src/index.js": true, "src/worker.js": true}
	exists := func(rel string) bool { return staged[rel] }

	for _, tt := range []struct {
		name, in, want string
	}{
		{
			name: "a .ts main names the .js Bazel staged",
			in:   "{\n  // the entry\n  \"main\": \"src/index.ts\",\n  \"compatibility_date\": \"2026-01-01\"\n}",
			want: "{\n  // the entry\n  \"main\": \"src/index.js\",\n  \"compatibility_date\": \"2026-01-01\"\n}",
		},
		{
			name: "a .tsx main too",
			in:   `{"main": "src/worker.tsx"}`,
			want: `{"main": "src/worker.js"}`,
		},
		{
			name: "a main already naming .js is left alone",
			in:   `{"main": "src/index.js"}`,
			want: `{"main": "src/index.js"}`,
		},
		{
			name: "a .ts main with no compiled sibling is left alone, so wrangler reports it",
			in:   `{"main": "src/missing.ts"}`,
			want: `{"main": "src/missing.ts"}`,
		},
		{
			name: "a leading ./ survives the rewrite",
			in:   `{"main": "./src/index.ts"}`,
			want: `{"main": "./src/index.js"}`,
		},
		{
			name: "no main at all",
			in:   `{"name": "w"}`,
			want: `{"name": "w"}`,
		},
		{
			name: "a string that merely contains main is not the key",
			in:   `{"vars": {"DOMAIN": "main.example.com"}}`,
			want: `{"vars": {"DOMAIN": "main.example.com"}}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := retargetMain(tt.in, exists); got != tt.want {
				t.Errorf("retargetMain:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func wranglerFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	return fakeRunfiles(t, map[string]string{
		"_main/tests/app/wrangler.jsonc":                            "{}",
		"_main/tests/app/src/index.js":                              "export default {}",
		"_main/tests/app/wrangler_modules":                          dirMarker,
		"_main/tests/app/wrangler_modules/wrangler/bin/wrangler.js": "x",
		"+node+/bin/node":                                           "#!/bin/sh\n",
	})
}

func wranglerConfig() *Config {
	return &Config{
		Label:   "//tests/app:deploy_dry_run",
		Mode:    ModeWrangler,
		Runtime: "+node+/bin/node",
		Wrangler: &WranglerConfig{
			ConfigFile:     "_main/tests/app/wrangler.jsonc",
			NodeModules:    "_main/tests/app/wrangler_modules",
			WranglerInTree: "wrangler/bin/wrangler.js",
			WorkerFiles:    []string{"_main/tests/app/src/index.js"},
			PackagePrefix:  "_main/tests/app/",
		},
	}
}

func TestPlanWranglerDeploysTheNamedEnvironment(t *testing.T) {
	r, real := wranglerFixture(t)
	cfg := wranglerConfig()
	cfg.Wrangler.EnvName = "staging"
	plan, err := MakePlan(cfg, r, []string{"--env", "production"})
	if err != nil {
		t.Fatal(err)
	}
	// The command line comes last, so `bazel run -- --env x` still wins.
	want := []string{
		real["+node+/bin/node"],
		// wrangler runs through the link beside the staged worker, not from its
		// own runfiles path, so that a package it imports is the link's sibling.
		filepath.Join(plan.Dir, "node_modules", "wrangler", "bin", "wrangler.js"),
		"deploy", "--dry-run", "--outdir", filepath.Join(plan.Dir, "dist"),
		"-c", filepath.Join(plan.Dir, "wrangler.jsonc"),
		"--env", "staging", "--env", "production",
	}
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q, want %q", plan.Argv, want)
	}
}

func TestPlanWranglerNamesNoEnvironmentWhenTheAttrIsEmpty(t *testing.T) {
	r, _ := wranglerFixture(t)
	plan, err := MakePlan(wranglerConfig(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plan.Argv, "--env") {
		t.Errorf("argv = %q, want no --env at all", plan.Argv)
	}
}
