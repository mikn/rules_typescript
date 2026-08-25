// Package dev_server_test starts a ts_dev_server and asks it questions.
//
// The generated launcher and vite.config.mjs are not the deliverable; a running
// dev server that serves the right bytes is. So this runs the launcher exactly
// as `bazel run` does -- RUNFILES_DIR plus BUILD_WORKSPACE_DIRECTORY -- against
// a throwaway workspace, and asserts over HTTP:
//
//  1. the generated config, EVALUATED (it is a module that reads its
//     environment and the filesystem), configures the port the rule was given,
//     an allow-list that reaches bazel-bin, a watch path ibazel's rebuilds land
//     in, an alias per first-party module_name pointing at SOURCE, and the
//     inputs a rebuild has to restart the server for;
//  2. the running server serves a file from bazel-bin rather than answering 403;
//  3. an `import "./app.ts"` lands on the .ts SOURCE in both variants: dev
//     takes Bazel out of the inner loop, so Vite transforms first-party source
//     itself. What the plugin adds is bazel-bin for what Vite cannot produce:
//     a ts_codegen output with no checked-in source resolves WITH the plugin
//     and does not without it. Same request, two answers -- which is what
//     proves the plugin is installed and resolving rather than merely named in
//     the config text;
//  4. a first-party bare specifier (`@devserver/lib`) resolves to that
//     package's source, so it is one module in the graph with a relative
//     import of the same file;
//  5. with react_refresh = True, a .tsx module comes back carrying the React
//     Fast Refresh preamble; without it, it does not;
//  6. the launcher survives the SIGTERM ibazel sends on every rebuild, and the
//     server behind it keeps answering;
//  7. a rebuild that only rewrote ts_codegen output serves the new bytes and
//     does NOT restart Vite; a rebuild that changed the generated config does;
//  8. a BARE npm specifier out of first-party source resolves into the Bazel npm
//     tree and the file it lands on is served. Vite has no search-path option,
//     so this is the bazel:npm-resolve plugin or nothing -- and a package that
//     is not in the tree still fails, so the plugin is resolving rather than
//     inventing;
//  9. with a vite_config, the user's plugin is first in the container and its
//     transform reaches the response, which also means its own bare npm import
//     resolved.
//
// Which variant is under test comes from the env of the go_test target:
// DEV_TARGET, DEV_PORT, DEV_BAZEL_PLUGIN, DEV_REACT_REFRESH, DEV_USER_CONFIG.
package dev_server_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mikn/rules_typescript/tests/verify"
)

func TestDevServerBehaviour(t *testing.T) {
	tree := verify.New(t)
	target := env(t, "DEV_TARGET")
	wantPort := env(t, "DEV_PORT")
	wantBazelPlugin := env(t, "DEV_BAZEL_PLUGIN") == "1"
	wantReactRefresh := env(t, "DEV_REACT_REFRESH") == "1"
	wantUserConfig := env(t, "DEV_USER_CONFIG") == "1"
	impl := env(t, "DEV_IMPL")

	node := tree.File("ts/toolchain/node_resolved/node")
	launcher := tree.File("tests/dev_server/" + target + "_launcher")
	config := tree.File("tests/dev_server/" + target + "_dev/vite.config.mjs")
	readConfig := tree.File("tests/dev_server/read_config.mjs")
	found := true
	for _, f := range []verify.File{node, launcher, config, readConfig} {
		if !f.Exists() {
			found = false
		}
	}
	if !found {
		t.FailNow()
	}

	// app.ts and bazel-bin/app.js carry different marker strings, which is how
	// the response tells us which of the two the server chose.
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "ws")
	bazelBin := filepath.Join(ws, "bazel-bin")
	mkdir(t, bazelBin)
	write(t, filepath.Join(ws, "app.ts"), "export const origin: string = \"TS_SOURCE_TRANSFORMED_BY_VITE\";\n")
	write(t, filepath.Join(bazelBin, "app.js"), "export const origin = \"JS_PRECOMPILED_BY_BAZEL\";\n")
	write(t, filepath.Join(ws, "entry.js"), "import { origin } from \"./app.ts\";\nexport { origin };\n")
	// No JSX: what is under test is the Fast Refresh transform, and JSX would pull
	// in react/jsx-runtime, which needs a react this npm tree does not carry.
	write(t, filepath.Join(ws, "widget.tsx"), "export function Widget() {\n  return null;\n}\n")

	// A bare npm specifier, and one for a package no tree has.
	write(t, filepath.Join(ws, "npm_entry.js"), "import { z } from \"zod\";\nexport { z };\n")
	write(t, filepath.Join(ws, "npm_missing.js"),
		"import x from \"not-in-any-npm-tree\";\nexport { x };\n")

	// The package //tests/dev_server/lib declares module_name "@devserver/lib".
	// Its source has to be where the config expects it for the alias to point at
	// source rather than at bazel-bin.
	libSource := filepath.Join(ws, "tests", "dev_server", "lib", "index.ts")
	mkdir(t, filepath.Dir(libSource))
	write(t, libSource, "export const packageName: string = \"LIB_SOURCE_TRANSFORMED_BY_VITE\";\n")
	write(t, filepath.Join(ws, "alias_entry.js"),
		"import { packageName } from \"@devserver/lib\";\nexport { packageName };\n")

	// A ts_codegen output: generated .ts under bazel-bin with no source in the
	// workspace. Vite cannot produce it, so this is what bazel-bin is still for.
	generated := filepath.Join(bazelBin, "generated", "routes.ts")
	mkdir(t, filepath.Dir(generated))
	write(t, generated, "export const routes: string[] = [\"CODEGEN_V1\"];\n")
	write(t, filepath.Join(ws, "gen_entry.js"),
		"import { routes } from \"./generated/routes.ts\";\nexport { routes };\n")

	// The config watches its own bazel-bin copy, so the scratch workspace needs
	// one for the restart decision to have a baseline to move away from.
	scratchConfig := filepath.Join(bazelBin, "tests", "dev_server", target+"_dev", "vite.config.mjs")
	mkdir(t, filepath.Dir(scratchConfig))
	write(t, scratchConfig, "// stand-in for the generated config, v1\n")

	// ── 1: what the config says, by running it ────────────────────────────────
	cfg := evalConfig(t, node.Abs(), readConfig.Abs(), config.Abs(),
		readLauncherConfig(t, tree, target).env(tree, ws))
	t.Logf("config = %s", cfg.raw)

	if got := strconv.Itoa(cfg.Port); got != wantPort {
		t.Errorf("the config serves port %s, but the rule was given %s", got, wantPort)
	}
	if cfg.Root != ws {
		t.Errorf("config root is %s, want the workspace root %s", cfg.Root, ws)
	}
	if !slices.Contains(cfg.FsAllow, bazelBin) {
		t.Errorf("server.fs.allow does not include %s: %v", bazelBin, cfg.FsAllow)
	}
	// ibazel writes rebuilt .js here, and Vite only notices through this watcher.
	if !slices.Contains(cfg.WatchPaths, bazelBin) {
		t.Errorf("server.watch.paths does not include %s: %v", bazelBin, cfg.WatchPaths)
	}

	// The alias is what makes `@devserver/lib` mean source in dev. It has to name
	// the source file, not the compiled output: the point of serving source is
	// that Bazel is not in the loop.
	if got := replacementFor(cfg.Alias, "/^@devserver\\/lib$/"); got != libSource {
		t.Errorf("resolve.alias sends @devserver/lib to %q, want the source %q\nalias = %v",
			got, libSource, cfg.Alias)
	}

	// Restart-or-keep, as the config declares it: the config itself is fixable by
	// an in-process restart, a new Vite or a new node binary is not, and no
	// ts_codegen output is on the list at all -- that is the whole point.
	wantInputs := map[string]string{scratchConfig: "restart"}
	for _, input := range cfg.ConfigInputs {
		if want, ok := wantInputs[input.Path]; ok {
			if input.Remedy != want {
				t.Errorf("input %s declares remedy %q, want %q", input.Path, input.Remedy, want)
			}
			delete(wantInputs, input.Path)
		}
		if strings.HasPrefix(input.Path, filepath.Join(bazelBin, "generated")) {
			t.Errorf("a ts_codegen output is a watched config input (%s); a codegen "+
				"rebuild would restart the dev server", input.Path)
		}
	}
	for path := range wantInputs {
		t.Errorf("configInputs does not watch %s: %v", path, cfg.ConfigInputs)
	}

	// npm resolution is a plugin, because Vite has no option for it: resolve.modules
	// is webpack's, and a config that sets it configures nothing.
	if !slices.Contains(cfg.Plugins, "bazel:npm-resolve") {
		t.Errorf("the config installs no bazel:npm-resolve plugin, so no bare npm "+
			"specifier can resolve: plugins = %v", cfg.Plugins)
	}
	if cfg.ResolveModules != nil {
		t.Errorf("the config sets resolve.modules = %v, which is a webpack option Vite "+
			"ignores; whatever it was meant to do is not being done", cfg.ResolveModules)
	}
	if got := slices.Contains(cfg.Plugins, "vite:react-babel"); got != wantReactRefresh {
		t.Errorf("react plugin present = %v, want %v: plugins = %v",
			got, wantReactRefresh, cfg.Plugins)
	}
	// A framework transform has to see a module before the Bazel plugins do.
	if wantUserConfig {
		if len(cfg.Plugins) == 0 || cfg.Plugins[0] != "devserver-user-config" {
			t.Errorf("the vite_config plugin is not first in the container: plugins = %v",
				cfg.Plugins)
		}
	}

	// Args past the launcher reach the server's own CLI, which is not a shared
	// surface: --strictPort is Vite's, and oj rejects it outright.
	var extraArgs []string
	if impl == "vite" {
		extraArgs = append(extraArgs, "--strictPort")
	}
	srv := start(t, launcher.Abs(), ws, tmp, extraArgs...)
	base := srv.awaitHTTP(t)
	t.Logf("%s (%s) is up and answering on %s", target, impl, base)

	// ── 2: the running server really serves bazel-bin ─────────────────────────
	// Vite answers 403 for a file outside fs.allow, so a 200 here is the whole
	// assertion: without the generated allow-list this request is denied.
	t.Run("serves_bazel_bin", func(t *testing.T) {
		r := get(t, base, "/@fs"+bazelBin+"/app.js")
		if r.status != 200 {
			t.Errorf("serving from bazel-bin returned %d, want 200 (server.fs.allow lost bazel-bin)\nbody:\n%s",
				r.status, r.body)
		}
	})

	// ── 3: where an import of a .ts module lands ──────────────────────────────
	// Vite rewrites every import specifier in what it serves to the URL it
	// resolved, so the served entry.js says which file the plugin container
	// chose for "./app.ts". In dev that must be the SOURCE in both variants:
	// Bazel is out of the inner loop, and the pre-compiled .js next to it is
	// exactly the round trip this is meant to skip.
	t.Run("serves_first_party_source", func(t *testing.T) {
		r := get(t, base, "/entry.js")
		if r.status != 200 {
			t.Fatalf("GET /entry.js returned %d, want 200\n%s", r.status, r.body)
		}
		t.Logf("entry.js = %s", r.body)
		r.contains(t, srv, "/app.ts")
		r.excludes(t, "bazel-bin/app.js")

		// And the bytes really came from Vite transforming the source.
		m := get(t, base, "/app.ts")
		m.contains(t, srv, "TS_SOURCE_TRANSFORMED_BY_VITE")
		m.excludes(t, "JS_PRECOMPILED_BY_BAZEL")
	})

	// ── 3b: what bazel-bin is still for ───────────────────────────────────────
	// A ts_codegen output has no checked-in source, so Vite cannot produce it and
	// the plugin has to find it under bazel-bin. Without the plugin the same
	// import resolves nowhere -- the one request that separates the two variants.
	t.Run("resolves_codegen_from_bazel_bin", func(t *testing.T) {
		r := get(t, base, "/gen_entry.js")
		t.Logf("gen_entry.js (status %d) = %s", r.status, r.body)
		if !wantBazelPlugin {
			// Neither server can see bazel-bin without the plugin; they differ in
			// when they say so. Vite fails the transform, oj serves the module with
			// the specifier untouched so the failure lands in the browser instead.
			if impl == "oj" {
				r.contains(t, srv, "./generated/routes.ts")
				r.excludes(t, "bazel-bin/generated/routes.ts")
				return
			}
			if r.status == 200 {
				t.Fatalf("GET /gen_entry.js returned 200 without the plugin; bazel-bin "+
					"is not Vite's to resolve\n%s", r.body)
			}
			r.contains(t, srv, "Failed to resolve import")
			r.excludes(t, "bazel-bin/generated/routes.ts")
			return
		}
		if r.status != 200 {
			t.Fatalf("GET /gen_entry.js returned %d, want 200\n%s", r.status, r.body)
		}
		r.contains(t, srv, "bazel-bin/generated/routes.ts")
		m := get(t, base, "/@fs"+filepath.Join(bazelBin, "generated", "routes.ts"))
		m.contains(t, srv, "CODEGEN_V1")
	})

	// ── 4: a first-party bare specifier ───────────────────────────────────────
	// `@devserver/lib` is how packages import each other. resolve.alias is a rule
	// feature, not a plugin one, so both variants must land on the same source
	// file -- otherwise the bare import and a relative import of the same file
	// are two modules in Vite's graph.
	t.Run("resolves_first_party_bare_specifier", func(t *testing.T) {
		r := get(t, base, "/alias_entry.js")
		if r.status != 200 {
			t.Fatalf("GET /alias_entry.js returned %d, want 200\n%s", r.status, r.body)
		}
		t.Logf("alias_entry.js = %s", r.body)
		r.contains(t, srv, "/tests/dev_server/lib/index.ts")
		m := get(t, base, "/tests/dev_server/lib/index.ts")
		m.contains(t, srv, "LIB_SOURCE_TRANSFORMED_BY_VITE")
	})

	// ── 4b: a bare npm specifier ──────────────────────────────────────────────
	// The npm tree is a Bazel output, and nothing above a checked-in source file
	// is a node_modules directory, so without the resolver plugin this request is
	// an unresolved-import error. The response says which file Vite chose, and
	// that file has to be served too -- resolving into a directory server.fs.allow
	// does not reach would only move the failure one request later.
	t.Run("resolves_npm_from_bazel_tree", func(t *testing.T) {
		r := get(t, base, "/npm_entry.js")
		if r.status != 200 {
			t.Fatalf("GET /npm_entry.js returned %d, want 200\n%s\n%s", r.status, r.body, srv.log(t))
		}
		t.Logf("npm_entry.js = %s", r.body)
		dep := depURL(r.body)
		if dep == "" {
			t.Fatalf("nothing in the response points at a resolved dependency:\n%s", r.body)
		}
		m := get(t, base, dep)
		if !strings.Contains(m.finalURL, "/node_modules/zod/") {
			t.Errorf("`import \"zod\"` resolved to %q, which is not in a Bazel npm tree",
				m.finalURL)
		}
		if m.status != 200 {
			t.Errorf("the resolved dependency %s answers %d, want 200\n%s", dep, m.status, m.body)
		}

		// And the plugin resolves rather than invents: a package no tree has must
		// not come back as anything. Where that failure surfaces differs -- Vite
		// resolves while transforming and fails the module, oj defers to a
		// container URL and fails when it is requested -- so the assertion is that
		// it fails, not when.
		missing := get(t, base, "/npm_missing.js")
		if missing.status != 200 {
			missing.contains(t, srv, "Failed to resolve import")
			return
		}
		deferred := depURL(missing.body)
		if deferred == "" {
			t.Fatalf("GET /npm_missing.js returned 200 and resolved `not-in-any-npm-tree` "+
				"to nothing deferred either; the specifier was invented\n%s", missing.body)
		}
		if answer := get(t, base, deferred); answer.status == 200 {
			t.Errorf("the deferred reference for `not-in-any-npm-tree` answers 200 from %q; "+
				"a package no tree has must not resolve\n%s", answer.finalURL, answer.body)
		}
	})

	// ── 4c: the user-supplied vite_config ─────────────────────────────────────
	// The marker reaches the response only if the config loaded, its own bare npm
	// import resolved, and its plugin is in the container.
	t.Run("user_config_plugin", func(t *testing.T) {
		r := get(t, base, "/app.ts")
		if r.status != 200 {
			t.Fatalf("GET /app.ts returned %d, want 200\n%s", r.status, r.body)
		}
		if !wantUserConfig {
			r.excludes(t, "USER_CONFIG_PLUGIN_RAN")
			return
		}
		r.contains(t, srv, "USER_CONFIG_PLUGIN_RAN")
	})

	// ── 5: React Fast Refresh ─────────────────────────────────────────────────
	t.Run("react_refresh", func(t *testing.T) {
		r := get(t, base, "/widget.tsx")
		if r.status != 200 {
			t.Fatalf("GET /widget.tsx returned %d, want 200\n%s\n%s", r.status, r.body, srv.log(t))
		}
		if impl == "oj" {
			// oj applies Fast Refresh itself, which is why ts_dev_server rejects
			// react_refresh = True against it rather than stacking plugin-react on
			// top. The transform is oj's own, so the plugin-react preamble that the
			// Vite variants assert on is not what shows up here.
			r.contains(t, srv, "$RefreshReg$")
			return
		}
		if !wantReactRefresh {
			r.excludes(t, "react-refresh", "$RefreshReg$")
			return
		}
		// The plugin has to have loaded AND run: the preamble is what preserves
		// component state, and plugin-react only emits it after resolving the
		// react-refresh runtime out of the same npm tree.
		r.contains(t, srv, "/@react-refresh", "$RefreshReg$")
	})

	// ── 6: the SIGTERM ibazel sends on every rebuild ──────────────────────────
	// ibazel terminates the launcher and rebuilds; the point of the dev server is
	// that vite lives through it and picks the new .js up from its watcher.
	t.Run("survives_ibazel_sigterm", func(t *testing.T) {
		if err := srv.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("SIGTERM to the launcher: %v", err)
		}
		for i := 0; i < 10; i++ {
			if done, err := srv.exited(); done {
				t.Fatalf("the launcher died on SIGTERM (%v); ibazel would take the dev server "+
					"down on every rebuild\n%s", err, srv.log(t))
			}
			time.Sleep(100 * time.Millisecond)
		}
		if r := get(t, base, "/app.ts"); r.status != 200 {
			t.Errorf("after SIGTERM the server answers %d, want 200\n%s", r.status, srv.log(t))
		}
	})

	// ── 7: restart-or-keep, both ways ─────────────────────────────────────────
	// Only the plugin makes the decision; without it there is no ConfigWatcher
	// and nothing to assert.
	if !wantBazelPlugin {
		return
	}

	// A rebuild that only rewrote ts_codegen output leaves the running server
	// correctly configured, so it must serve the new bytes without restarting.
	t.Run("keeps_running_on_codegen_rebuild", func(t *testing.T) {
		before := restartCount(t, srv)
		write(t, generated, "export const routes: string[] = [\"CODEGEN_V2\"];\n")
		url := "/@fs" + filepath.Join(bazelBin, "generated", "routes.ts")
		if !eventually(t, func() bool {
			return strings.Contains(bodyOf(base, url), "CODEGEN_V2")
		}) {
			t.Errorf("the rebuilt codegen output never reached the server\n%s", srv.log(t))
		}
		if after := restartCount(t, srv); after != before {
			t.Errorf("a codegen-only rebuild restarted Vite (%d → %d restarts)\n%s",
				before, after, srv.log(t))
		}
	})

	// A rebuild that changed the generated config left the running server
	// configured for a graph that no longer exists, and only a restart fixes it.
	t.Run("restarts_on_config_change", func(t *testing.T) {
		before := restartCount(t, srv)
		write(t, scratchConfig, "// stand-in for the generated config, v2 — new aliases\n")
		if !eventually(t, func() bool { return restartCount(t, srv) > before }) {
			t.Fatalf("the config changed and Vite did not restart\n%s", srv.log(t))
		}
		if !eventually(t, func() bool { return answers(base, "/app.ts") }) {
			t.Errorf("the server never came back after restarting\n%s", srv.log(t))
		}
	})
}

// depURL is the first URL a served module imports, in whichever form the server
// rewrites to: Vite names the resolved file directly (/@fs/<abs>), oj names an
// id its plugin container resolved (/@id/<hex>) and redirects to the file. Both
// are "the URL this module's dependency is at"; where it lands is the assertion,
// not how it is spelled.
func depURL(body string) string {
	m := regexp.MustCompile(`"(/@(?:fs|id)/[^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// restartCount is how many times the plugin has decided the running server is
// configured for a graph that no longer exists.
func restartCount(t *testing.T, s *server) int {
	t.Helper()
	return strings.Count(s.log(t), "[vite-plugin-bazel] restarting:")
}

func eventually(t *testing.T, done func() bool) bool {
	t.Helper()
	for i := 0; i < 60; i++ {
		if done() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is unset; the go_test target must set it", name)
	}
	return v
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type devConfig struct {
	Port         int           `json:"port"`
	Host         any           `json:"host"`
	Root         string        `json:"root"`
	FsAllow      []string      `json:"fsAllow"`
	WatchPaths   []string      `json:"watchPaths"`
	Alias        []aliasEntry  `json:"alias"`
	ConfigInputs []configInput `json:"configInputs"`
	Plugins      []string      `json:"plugins"`
	// resolve.modules is webpack's, not Vite's. Read back so a test can fail if
	// it returns.
	ResolveModules any `json:"resolveModules"`
	raw            string
}

type aliasEntry struct {
	Find        string `json:"find"`
	Replacement string `json:"replacement"`
}

type configInput struct {
	Label  string `json:"label"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Remedy string `json:"remedy"`
}

// replacementFor returns what the alias with this exact `find` points at, or ""
// when the config declared no such alias.
func replacementFor(entries []aliasEntry, find string) string {
	for _, entry := range entries {
		if entry.Find == find {
			return entry.Replacement
		}
	}
	return ""
}

// evalConfig runs the generated config as the module it is: the port, root,
// allow-list and plugin list it reports are all read out of its environment at
// import time, so it gets the environment the launcher would have given it.
func evalConfig(t *testing.T, node, readConfig, config string, env []string) devConfig {
	t.Helper()
	cmd := exec.Command(node, readConfig, config)
	cmd.Env = append(os.Environ(), env...)
	// stderr separately: the JSON is on stdout, and the reason it is not there is
	// on stderr.
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("evaluating %s: %v\n%s%s", config, err, out, stderr.String())
	}
	cfg := devConfig{raw: string(out)}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("read_config.mjs printed %q, which is not JSON: %v", out, err)
	}
	return cfg
}

type server struct {
	cmd     *exec.Cmd
	wait    chan error
	logPath string
	port    int
}

// start runs the launcher the way `bazel run` does, in its own process group so
// the whole tree can be signalled: the launcher ignores TERM (that is how it
// survives ibazel restarts) and vite is its child.
//
// The server listens on a kernel-assigned port: on the configured one, two
// concurrent `bazel test` runs make Vite increment past the collision and one run
// then answers the other's requests. --strictPort keeps that from happening
// quietly.
func start(t *testing.T, launcher, ws, tmp string, extraArgs ...string) *server {
	t.Helper()
	port := freePort(t)
	t.Logf("serving on port %d", port)

	logPath := filepath.Join(tmp, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create %s: %v", logPath, err)
	}
	defer logFile.Close()

	cmd := exec.Command(launcher, append([]string{"--port", strconv.Itoa(port)}, extraArgs...)...)
	cmd.Dir = ws
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"RUNFILES_DIR="+runfilesDir(t),
		"BUILD_WORKSPACE_DIRECTORY="+ws,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", launcher, err)
	}

	srv := &server{cmd: cmd, wait: make(chan error, 1), logPath: logPath, port: port}
	go func() { srv.wait <- cmd.Wait() }()
	t.Cleanup(srv.stop)
	return srv
}

func (s *server) stop() {
	pgid := s.cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGINT)
	for i := 0; i < 20; i++ {
		if done, _ := s.exited(); done {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	<-s.wait
}

// exited reports whether the launcher has already stopped, leaving the wait
// result in place for whoever asks next.
func (s *server) exited() (bool, error) {
	select {
	case err := <-s.wait:
		s.wait <- err
		return true, err
	default:
		return false, nil
	}
}

func (s *server) log(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(s.logPath)
	if err != nil {
		return "server log unreadable: " + err.Error()
	}
	return "--- server log ---\n" + string(raw)
}

// awaitHTTP returns the base URL that answers. server.host is "localhost", which
// resolves to whichever family the sandbox offers, so the base URL is whichever
// of these three answers first.
func (s *server) awaitHTTP(t *testing.T) string {
	t.Helper()
	var lastErr error
	for i := 0; i < 120; i++ {
		for _, host := range []string{"localhost", "127.0.0.1", "[::1]"} {
			base := "http://" + host + ":" + strconv.Itoa(s.port)
			_, err := httpGet(base + "/app.ts")
			if err == nil {
				return base
			}
			lastErr = err
		}
		if done, err := s.exited(); done {
			t.Fatalf("the dev server exited before it accepted a connection (%v)\n%s", err, s.log(t))
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no HTTP response on port %d after 60s (last error: %v)\n%s", s.port, lastErr, s.log(t))
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// response mirrors verify.File's report-and-continue assertions over an HTTP
// body, which is not a file in the runfiles tree and so cannot go through it.
type response struct {
	url    string
	status int
	body   string
	// finalURL is where the request landed after redirects. A dev server may
	// answer a resolved-dependency URL with one -- oj serves an id its plugin
	// container resolved by redirecting to that file's own URL -- and which file
	// it chose is only visible here.
	finalURL string
}

func get(t *testing.T, base, path string) response {
	t.Helper()
	url := base + path
	r, err := httpGet(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return r
}

// bodyOf is the served body, or "" when the request did not land. Same reason as
// `answers`: inside a poll a transport error is a "not yet".
func bodyOf(base, path string) string {
	r, err := httpGet(base + path)
	if err != nil {
		return ""
	}
	return r.body
}

// answers reports whether the server served this path, treating a transport
// error as "not yet" rather than as a failure.
//
// `get` fatals on one, which is right for a single request -- there is no server
// and the test cannot continue -- and wrong inside a poll for a server that is
// deliberately being restarted: a request landing on the socket the old process
// is closing is exactly what the poll is waiting through, and fatalling on it
// fails the test for the condition it was told to expect.
func answers(base, path string) bool {
	r, err := httpGet(base + path)
	return err == nil && r.status == 200
}

func httpGet(url string) (response, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return response{url: url}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response{url: url, status: resp.StatusCode}, err
	}
	final := url
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return response{url: url, status: resp.StatusCode, body: string(body), finalURL: final}, nil
}

func (r response) contains(t *testing.T, s *server, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(r.body, want) {
			t.Errorf("%s (status %d) does not contain %q\nbody:\n%s\n%s",
				r.url, r.status, want, r.body, s.log(t))
		}
	}
}

func (r response) excludes(t *testing.T, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(r.body, bad) {
			t.Errorf("%s (status %d) contains %q, and must not\nbody:\n%s",
				r.url, r.status, bad, r.body)
		}
	}
}
