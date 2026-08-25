package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigRejectsBadDocuments(t *testing.T) {
	cases := map[string]string{
		"missing mode":        `{"label":"//a:b"}`,
		"unknown mode":        `{"label":"//a:b","mode":"bash"}`,
		"node without entry":  `{"label":"//a:b","mode":"node","node":{}}`,
		"node without body":   `{"label":"//a:b","mode":"node"}`,
		"vitest without body": `{"label":"//a:b","mode":"vitest"}`,
		"devserver no config": `{"label":"//a:b","mode":"devserver","dev_server":{}}`,
		// Exactly one of the two server forms: a config naming both leaves which
		// executable actually serves the app up to whichever the launcher reads
		// first, and naming neither has nothing to run at all.
		"devserver both server forms": `{"label":"//a:b","mode":"devserver","dev_server":{"config_file":"c","server_binary":"b","server_in_tree":"vite/bin/vite.js"}}`,
		"devserver no server form":    `{"label":"//a:b","mode":"devserver","dev_server":{"config_file":"c"}}`,
		"unknown field":               `{"label":"//a:b","mode":"node","node":{"entry":"a"},"shell":"bash"}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig([]byte(doc)); err == nil {
				t.Fatalf("ParseConfig(%s) accepted an invalid config", doc)
			}
		})
	}
}

func TestParseConfigKeepsValuesShellWouldHaveMangled(t *testing.T) {
	doc := `{
	  "label": "//a:b",
	  "mode": "node",
	  "workspace": "_main",
	  "runtime_args": ["--title=$(id -u)", "--x=` + "`" + `whoami` + "`" + `"],
	  "env": {"MSG": "a \"quoted\" $HOME \\ value"},
	  "node": {"entry": "_main/a/b.js"}
	}`
	cfg, err := ParseConfig([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.RunArgs[0], "--title=$(id -u)"; got != want {
		t.Errorf("runtime_args[0] = %q, want %q", got, want)
	}
	if got, want := cfg.Env["MSG"], `a "quoted" $HOME \ value`; got != want {
		t.Errorf("env[MSG] = %q, want %q", got, want)
	}
}

func TestConfigPathFindsSiblingOfArgv0(t *testing.T) {
	t.Setenv(ConfigEnvVar, "")
	dir := t.TempDir()
	launcher := filepath.Join(dir, "app_launcher")
	if err := os.WriteFile(launcher+".json", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := configPath(launcher, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != launcher+".json" {
		t.Errorf("configPath = %q, want %q", got, launcher+".json")
	}
}

func TestConfigPathPrefersEnvOverride(t *testing.T) {
	t.Setenv(ConfigEnvVar, "/somewhere/explicit.json")
	got, err := configPath("/ignored/app_launcher", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/somewhere/explicit.json" {
		t.Errorf("configPath = %q, want the %s override", got, ConfigEnvVar)
	}
}

func TestConfigPathErrorNamesWhatItTried(t *testing.T) {
	t.Setenv(ConfigEnvVar, "")
	_, err := configPath(filepath.Join(t.TempDir(), "app_launcher"), nil)
	if err == nil {
		t.Fatal("configPath succeeded with no config on disk")
	}
	if !strings.Contains(err.Error(), "app_launcher.json") || !strings.Contains(err.Error(), ConfigEnvVar) {
		t.Errorf("error is not actionable: %v", err)
	}
}

func TestConfigPathFindsTheRootSymlinkInRunfiles(t *testing.T) {
	t.Setenv(ConfigEnvVar, "")
	r, real := fakeRunfiles(t, map[string]string{
		"app_launcher.json": `{"label":"//a:b","mode":"node","node":{"entry":"x"}}`,
	})
	// argv[0] is an exec-root path with no config beside it, which is how a
	// launcher used as a tool inside another rule's action is invoked.
	got, err := configPath("bazel-out/k8-opt-exec/bin/external/pkg/app_launcher", r)
	if err != nil {
		t.Fatal(err)
	}
	if got != real["app_launcher.json"] {
		t.Errorf("configPath = %q, want %q", got, real["app_launcher.json"])
	}
}
