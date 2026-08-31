package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// Everything below builds a Plan and asserts on its fields. Nothing here runs
// wrangler: MakePlan decides an argv and an environment and returns them, and no
// test in this file hands either to a process. That is deliberate -- the deploy
// path uploads to Cloudflare, so its argv is verified as data and never fired.

func wranglerFixture(t *testing.T) (*Resolver, map[string]string) {
	t.Helper()
	r, real := fakeRunfiles(t, map[string]string{
		"+node+/bin/node":                    "#!/bin/sh\n",
		"_main/tests/workers/wrangler.jsonc": `{"name": "w", "main": "src/index.js"}`,
		"_main/tests/workers/src/index.js":   "export default {}",
		"_main/tests/workers/node_modules":   dirMarker,
	})
	entry := filepath.Join(real["_main/tests/workers/node_modules"], "wrangler", "bin", "wrangler.js")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("// wrangler"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_TMPDIR", t.TempDir())
	return r, real
}

func wranglerConfig(command string) *Config {
	return &Config{
		Label:   "//tests/workers:w",
		Mode:    ModeWrangler,
		Runtime: "+node+/bin/node",
		Wrangler: &WranglerConfig{
			Command:        command,
			ConfigFile:     "_main/tests/workers/wrangler.jsonc",
			NodeModules:    "_main/tests/workers/node_modules",
			WranglerInTree: "wrangler/bin/wrangler.js",
			WorkerFiles:    []string{"_main/tests/workers/src/index.js"},
			PackagePrefix:  "_main/tests/workers/",
		},
	}
}

// wranglerArgv is the argv the launcher should build, for a scratch dir of plan.Dir.
func wranglerArgv(node, scratch string, dryRun bool) []string {
	argv := []string{node, filepath.Join(scratch, "node_modules", "wrangler", "bin", "wrangler.js"), "deploy"}
	if dryRun {
		argv = append(argv, "--dry-run")
	}
	return append(argv, "--outdir", filepath.Join(scratch, "dist"), "-c", filepath.Join(scratch, "wrangler.jsonc"))
}

func TestPlanWranglerDeploysTheNamedEnvironment(t *testing.T) {
	r, real := wranglerFixture(t)
	cfg := wranglerConfig("")
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
	plan, err := MakePlan(wranglerConfig(""), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plan.Argv, "--env") {
		t.Errorf("argv = %q, want no --env at all", plan.Argv)
	}
}

// _ATTRS is shared by all three rules, so env_name reaches a deploy as well: the
// argv is the dry run's with --dry-run gone and the same --env still named.
func TestPlanWranglerDeployNamesTheEnvironmentToo(t *testing.T) {
	r, real := wranglerFixture(t)
	cfg := wranglerConfig(wranglerCommandDeploy)
	cfg.Wrangler.EnvName = "staging"
	plan, err := MakePlan(cfg, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := append(wranglerArgv(real["+node+/bin/node"], plan.Dir, false), "--env", "staging")
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q, want %q", plan.Argv, want)
	}
	if slices.Contains(plan.Argv, "--dry-run") {
		t.Error("a deploy still carries --dry-run and would upload nothing")
	}
}

// The one test standing between a future accident and production: anything that
// is not the exact deploy string dry-runs. The cases are the ways a config can
// arrive without saying "deploy" -- absent, explicit, misspelt, wrong case,
// whitespace, or a value from some other encoding of the same idea.
func TestPlanWranglerDryRunsUnlessTheConfigSaysDeployExactly(t *testing.T) {
	for _, command := range []string{
		"", "dry-run", "DEPLOY", "Deploy", "deploy ", " deploy", "deploy\n",
		"dry_run", "publish", "true", "1", "no-dry-run", "deployment",
	} {
		t.Run("command="+strconv.Quote(command), func(t *testing.T) {
			r, real := wranglerFixture(t)
			plan, err := MakePlan(wranglerConfig(command), r, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := wranglerArgv(real["+node+/bin/node"], plan.Dir, true)
			if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("argv = %q, want %q", plan.Argv, want)
			}
			if !slices.Contains(plan.Argv, "--dry-run") {
				t.Fatal("no --dry-run: this config would have uploaded to Cloudflare")
			}
			for _, key := range cloudflareCredentialVars {
				if !slices.Contains(plan.EnvUnset, key) {
					t.Errorf("%s reaches a dry run", key)
				}
			}
		})
	}
}

// The launcher dry-runs an unrecognised command, and ParseConfig refuses to load
// the config at all -- so a misspelt "deploy" is a loud failure, never a silent
// upload and never a silent downgrade either.
func TestParseConfigOnlyAcceptsTheTwoWranglerCommands(t *testing.T) {
	document := func(command string) []byte {
		cfg := `{"label": "//x:w", "mode": "wrangler", "wrangler": {` +
			`"config_file": "a", "node_modules": "b", "command": ` + strconv.Quote(command) + `}}`
		return []byte(cfg)
	}
	for _, command := range []string{"", "dry-run", "deploy"} {
		if _, err := ParseConfig(document(command)); err != nil {
			t.Errorf("command %q: %v", command, err)
		}
	}
	for _, command := range []string{"DEPLOY", "publish", "dry_run", "deploy ", "true"} {
		if _, err := ParseConfig(document(command)); err == nil {
			t.Errorf("command %q loaded; an unrecognised command must fail loudly", command)
		}
	}
}

// A config with no "command" key at all is the shape every launcher JSON written
// before this field existed has, and the shape a hand-written one has.
func TestParseConfigWithoutAWranglerCommandDryRuns(t *testing.T) {
	cfg, err := ParseConfig([]byte(
		`{"label": "//x:w", "mode": "wrangler", "wrangler": {"config_file": "a", "node_modules": "b"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wrangler.deploys() {
		t.Fatal("a config with no command would have uploaded")
	}
}

// The deploy argv is the dry-run argv with one flag taken out, and nothing else.
func TestPlanWranglerDeployArgvIsTheDryRunArgvWithoutTheFlag(t *testing.T) {
	r, real := wranglerFixture(t)
	plan, err := MakePlan(wranglerConfig(wranglerCommandDeploy), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := wranglerArgv(real["+node+/bin/node"], plan.Dir, false)
	if strings.Join(plan.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q, want %q", plan.Argv, want)
	}
	if slices.Contains(plan.Argv, "--dry-run") {
		t.Error("a deploy still carries --dry-run and would upload nothing")
	}
}

// Credentials pass through for a deploy and are taken away from a dry run. The
// value below is a placeholder; the launcher never reads or prints a real one.
func TestPlanWranglerReachesCredentialsOnlyForADeploy(t *testing.T) {
	const sentinel = "not-a-token"
	for _, tt := range []struct {
		command string
		reaches bool
	}{
		{wranglerCommandDryRun, false},
		{"", false},
		{wranglerCommandDeploy, true},
	} {
		t.Run("command="+strconv.Quote(tt.command), func(t *testing.T) {
			r, _ := wranglerFixture(t)
			t.Setenv("CLOUDFLARE_API_TOKEN", sentinel)
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", sentinel)
			t.Setenv("HOME", "/ambient/home")
			plan, err := MakePlan(wranglerConfig(tt.command), r, nil)
			if err != nil {
				t.Fatal(err)
			}
			env := Environ(plan.EnvOverrides, plan.EnvUnset, nil)
			for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"} {
				present := slices.ContainsFunc(env, func(entry string) bool {
					return strings.HasPrefix(entry, key+"=")
				})
				if present != tt.reaches {
					t.Errorf("%s present = %v, want %v", key, present, tt.reaches)
				}
			}
			// A dry run is also cut off from a `wrangler login` on disk; a deploy
			// is the only thing allowed to see the real HOME.
			home, overridden := plan.EnvOverrides["HOME"]
			if tt.reaches {
				if overridden {
					t.Errorf("a deploy redirected HOME to %q, hiding the OAuth login", home)
				}
				if _, ci := plan.EnvOverrides["CI"]; ci {
					t.Error("a deploy forced CI; whether wrangler may prompt is the caller's to state")
				}
			} else {
				if !overridden || !strings.HasPrefix(home, plan.Dir) {
					t.Errorf("HOME = %q, want a scratch directory under %q", home, plan.Dir)
				}
				if plan.EnvOverrides["XDG_CONFIG_HOME"] != home {
					t.Errorf("XDG_CONFIG_HOME = %q, want %q", plan.EnvOverrides["XDG_CONFIG_HOME"], home)
				}
			}
		})
	}
}

// wrangler parses with yargs, so every boolean flag has a negated form and a
// user argument could otherwise turn a dry-run target into a real upload.
func TestPlanWranglerRefusesAnArgumentThatUndoesTheDryRun(t *testing.T) {
	for _, arg := range []string{
		"--no-dry-run", "--dry-run=false", "--dry-run=0", "--dry-run=no", "--dry-run=off",
		"--dry-run:false",
	} {
		t.Run(arg, func(t *testing.T) {
			r, _ := wranglerFixture(t)
			if _, err := MakePlan(wranglerConfig(wranglerCommandDryRun), r, []string{arg}); err == nil {
				t.Fatalf("%s was accepted; a dry-run target would have uploaded", arg)
			}
		})
	}
}

// The safe direction stays open: a deploy target can be asked for a dry run, and
// the argument lands where wrangler's parser sees it last.
func TestPlanWranglerLetsAnArgumentDowngradeADeploy(t *testing.T) {
	r, _ := wranglerFixture(t)
	plan, err := MakePlan(wranglerConfig(wranglerCommandDeploy), r, []string{"--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Argv[len(plan.Argv)-1] != "--dry-run" {
		t.Errorf("argv ends %q, want the user argument last", plan.Argv[len(plan.Argv)-1])
	}
}

// The launcher says which of the three rules it is running, and says out loud
// when it is about to upload.
func TestPlanWranglerAnnouncesADeploy(t *testing.T) {
	r, _ := wranglerFixture(t)
	deploy, err := MakePlan(wranglerConfig(wranglerCommandDeploy), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(deploy.Messages, "\n")
	if !strings.Contains(joined, "ts_worker_deploy") || !strings.Contains(joined, "this is not a dry run") {
		t.Errorf("deploy messages = %q", deploy.Messages)
	}

	r, _ = wranglerFixture(t)
	dry, err := MakePlan(wranglerConfig(""), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(dry.Messages, "\n")
	if strings.Contains(joined, "ts_worker_deploy") || strings.Contains(joined, "not a dry run") {
		t.Errorf("dry-run messages = %q", dry.Messages)
	}
}
