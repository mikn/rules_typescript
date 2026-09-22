// Package dev_server_test runs the launcher as `bazel run` does against a
// throwaway workspace and asserts over HTTP; the env picks the variant.
package dev_server_test

import (
	"encoding/base64"
	"encoding/json"
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

	"github.com/mikn/rules_typescript/tests/hmrsocket"
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
	if wantBazelPlugin && impl != "oj" {
		physicalWorkspace := filepath.Join(tmp, "physical-ws")
		mkdir(t, physicalWorkspace)
		if err := os.Symlink(physicalWorkspace, ws); err != nil {
			t.Fatal(err)
		}
	}
	appRoot := filepath.Join(ws, "tests", "dev_server")
	mkdir(t, appRoot)
	bazelBin := filepath.Join(ws, "bazel-bin")
	appBin := filepath.Join(bazelBin, "tests", "dev_server")
	mkdir(t, appBin)
	if target == "dev_oj_source" {
		for _, rel := range []string{"dev_app.ts", "lib/index.ts"} {
			content, err := os.ReadFile(tree.File("tests/dev_server/" + rel).Abs())
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(appRoot, rel)
			mkdir(t, filepath.Dir(dest))
			write(t, dest, string(content))
		}
	}
	write(t, filepath.Join(ws, "index.html"), "WORKSPACE_INDEX")
	write(t, filepath.Join(appRoot, "index.html"), `<html>NESTED_APP_INDEX<script type="module" src="/entry.js"></script></html>`)
	write(t, filepath.Join(appRoot, "app.ts"), "export const origin: string = \"TS_SOURCE_TRANSFORMED_BY_VITE\";\n")
	write(t, filepath.Join(appBin, "app.js"), "export const origin = \"JS_PRECOMPILED_BY_BAZEL\";\n")
	write(t, filepath.Join(ws, "tests", "shared.ts"), `export const shared = "SIBLING_SOURCE";`)
	write(t, filepath.Join(appRoot, "sibling_entry.js"), `import { shared } from "../shared.ts"; export { shared };`)
	blocked := filepath.Join(ws, "tests", "blocked.ts")
	defaultDenied := filepath.Join(ws, "tests", ".env")
	if impl == "oj" {
		write(t, blocked, `export const blocked = "DENIED_FIXTURE";`)
		write(t, defaultDenied, "FIXTURE_ONLY=true")
		canonicalBlocked, err := filepath.EvalSymlinks(blocked)
		if err != nil {
			t.Fatal(err)
		}
		canonicalWorkspace, err := filepath.EvalSymlinks(ws)
		if err != nil {
			t.Fatal(err)
		}
		nativeConfig, err := json.Marshal(map[string]any{
			"server": map[string]any{"fs": map[string]any{
				"allow": []string{canonicalWorkspace},
				"deny":  []string{filepath.ToSlash(canonicalBlocked)},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(appRoot, "oj.config.json"), string(nativeConfig))
	}
	write(t, filepath.Join(appRoot, "entry.js"), "import { origin } from \"./app.ts\";\nexport { origin };\n")
	// No JSX: what is under test is the Fast Refresh transform, and JSX would pull
	// in react/jsx-runtime, which needs a react this npm tree does not carry.
	write(t, filepath.Join(appRoot, "widget.tsx"), "export function Widget() {\n  return null;\n}\n")

	// A bare npm specifier, and one for a package no tree has.
	write(t, filepath.Join(appRoot, "npm_entry.js"), "import { z } from \"zod\";\nexport { z };\n")
	write(t, filepath.Join(appRoot, "npm_missing.js"),
		"import x from \"not-in-any-npm-tree\";\nexport { x };\n")

	// A ts_codegen output: generated .ts under bazel-bin with no source in the
	// workspace. Vite cannot produce it, so this is what bazel-bin is still for.
	generated := filepath.Join(appBin, "generated", "routes.ts")
	mkdir(t, filepath.Dir(generated))
	write(t, generated, "export const routes: string[] = [\"CODEGEN_V1\"];\n")
	write(t, filepath.Join(appRoot, "gen_entry.js"),
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
	if cfg.Root != appRoot {
		t.Errorf("config root is %s, want the application root %s", cfg.Root, appRoot)
	}
	if !slices.Contains(cfg.FsAllow, bazelBin) {
		t.Errorf("server.fs.allow does not include %s: %v", bazelBin, cfg.FsAllow)
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
		if strings.HasPrefix(input.Path, filepath.Join(appBin, "generated")) {
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
	// surface: --strictPort is Vite's.
	var extraArgs []string
	if impl == "vite" {
		extraArgs = append(extraArgs, "--strictPort")
	}
	srv := start(t, launcher.Abs(), ws, tmp, extraArgs...)
	base := srv.awaitHTTP(t, "/app.ts")
	t.Logf("%s (%s) is up and answering on %s", target, impl, base)

	if target == "dev_oj_source" {
		t.Run("source_app_resolves_js_import_to_source_library_without_emit", func(t *testing.T) {
			app := get(t, base, "/dev_app.ts")
			if app.status != 200 {
				t.Fatalf("source app returned %d: %s", app.status, app.body)
			}
			match := regexp.MustCompile(`["']([^"']*lib/index[^"']*)["']`).FindStringSubmatch(app.body)
			if match == nil {
				t.Fatalf("source app has no library import: %s", app.body)
			}
			library := get(t, base, strings.TrimPrefix(match[1], "."))
			if library.status != 200 {
				t.Fatalf("source library returned %d: %s", library.status, library.body)
			}
			library.contains(t, srv, "@devserver/lib")
		})
	}

	if impl == "oj" {
		t.Run("application_config_denies_workspace_files", func(t *testing.T) {
			control := get(t, base, "/@fs"+filepath.Join(ws, "tests", "shared.ts"))
			if control.status != 200 {
				t.Fatalf("allowed sibling returned %d: %s", control.status, control.body)
			}
			control.contains(t, srv, "SIBLING_SOURCE")
			for _, file := range []string{blocked, defaultDenied} {
				r := get(t, base, "/@fs"+file)
				if r.status != 403 {
					t.Fatalf("GET %s returned %d, want 403: %s", file, r.status, r.body)
				}
				r.excludes(t, "DENIED_FIXTURE", "FIXTURE_ONLY")
			}
		})
	}

	index := get(t, base, "/")
	if index.status != 200 {
		t.Fatalf("GET / returned %d: %s", index.status, index.body)
	}
	index.contains(t, srv, "NESTED_APP_INDEX")
	index.excludes(t, "WORKSPACE_INDEX")

	// ── 2: the running server really serves bazel-bin ─────────────────────────
	// Vite answers 403 for a file outside fs.allow, so a 200 here is the whole
	// assertion: without the generated allow-list this request is denied.

	t.Run("serves_bazel_bin", func(t *testing.T) {
		r := get(t, base, "/@fs"+appBin+"/app.js")
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
		moduleURL := "/app.ts"
		if impl == "oj" && wantBazelPlugin {
			moduleURL = depURL(r.body)
			if moduleURL == "" {
				t.Fatalf("entry.js has no resolved import URL: %s", r.body)
			}
		} else {
			r.contains(t, srv, "/app.ts")
		}
		r.excludes(t, "bazel-bin/app.js")

		m := get(t, base, moduleURL)
		if m.status != 200 {
			t.Fatalf("GET %s returned %d, want 200: %s", moduleURL, m.status, m.body)
		}
		m.contains(t, srv, "TS_SOURCE_TRANSFORMED_BY_VITE")
		m.excludes(t, "JS_PRECOMPILED_BY_BAZEL")
	})

	t.Run("serves_sibling_package", func(t *testing.T) {
		r := get(t, base, "/sibling_entry.js")
		if r.status != 200 {
			t.Fatalf("sibling importer returned %d: %s", r.status, r.body)
		}
		url := depURL(r.body)
		if url == "" {
			t.Fatalf("no sibling import URL in %s", r.body)
		}
		module := get(t, base, url)
		if module.status != 200 {
			t.Fatalf("sibling URL %s returned %d", url, module.status)
		}
		module.contains(t, srv, "SIBLING_SOURCE")
	})

	// ── 3b: what bazel-bin is still for ───────────────────────────────────────
	// A ts_codegen output has no checked-in source, so Vite cannot produce it and
	// the plugin has to find it under bazel-bin. Without the plugin the same
	// import resolves nowhere -- the one request that separates the two variants.
	t.Run("resolves_codegen_from_bazel_bin", func(t *testing.T) {
		r := get(t, base, "/gen_entry.js")
		t.Logf("gen_entry.js (status %d) = %s", r.status, r.body)
		if !wantBazelPlugin {
			if impl == "oj" {
				r.contains(t, srv, "./generated/routes.ts")
				r.excludes(t, "bazel-bin/generated/routes.ts")
				return
			}
			// Vite cannot see bazel-bin without the plugin, and fails the transform.
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
		moduleURL := "/@fs" + filepath.Join(appBin, "generated", "routes.ts")
		if impl == "oj" {
			moduleURL = depURL(r.body)
			if moduleURL == "" {
				t.Fatalf("gen_entry.js has no resolved import URL: %s", r.body)
			}
		} else {
			r.contains(t, srv, moduleURL)
		}
		m := get(t, base, moduleURL)
		if m.status != 200 {
			t.Fatalf("GET %s returned %d, want 200: %s", moduleURL, m.status, m.body)
		}
		m.contains(t, srv, "CODEGEN_V1")
	})

	// ── 4b: a bare npm specifier ──────────────────────────────────────────────
	// The npm tree is a Bazel output; what puts it on the walk up from a
	// checked-in source file is the <workspace>/node_modules link the launcher
	// makes. The response says which file the server chose, and that file has to
	// be served too -- resolving into a directory server.fs.allow does not reach
	// would only move the failure one request later.
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
		// Vite may serve the dependency directly or from its pre-bundle cache.
		if !strings.Contains(m.finalURL, "/node_modules/zod/") &&
			!strings.Contains(m.finalURL, "/vite-cache/deps/") {
			t.Errorf("`import \"zod\"` resolved to %q, which is neither a Bazel npm "+
				"tree nor this target's dependency cache", m.finalURL)
		}
		if m.status != 200 {
			t.Errorf("the resolved dependency %s answers %d, want 200\n%s", dep, m.status, m.body)
		}

		// Missing imports may fail during transformation or on the later module request.
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

	if wantBazelPlugin {
		t.Run("generated_hmr_uses_served_url", func(t *testing.T) {
			file := filepath.Join(appBin, "hot.js")
			prefix := "GENERATED"
			if impl == "oj" {
				prefix = "DISK_ONLY"
			}
			write(t, file, `export const value = "`+prefix+`_V1"; if (import.meta.hot) import.meta.hot.accept();`)
			write(t, filepath.Join(appRoot, "hot_entry.js"), `import { value } from "./hot.js"; export { value };`)
			url := depURL(get(t, base, "/hot_entry.js").body)
			if url == "" {
				t.Fatal("generated module has no import URL")
			}
			initial := get(t, base, url)
			if initial.status != 200 {
				t.Fatalf("generated module returned %d: %s", initial.status, initial.body)
			}
			initial.contains(t, srv, "GENERATED_V1")
			url = strings.TrimPrefix(initial.finalURL, base)
			if impl == "oj" {
				initial.excludes(t, "DISK_ONLY")
				initial.contains(t, srv, "TRANSFORM_ONE")
				initial.contains(t, srv, "TRANSFORM_TWO")
				initial.contains(t, srv, "APPLICATION_GRAPH_URL=/app.ts")
				initial.contains(t, srv, "__oj_createHotContext("+strconv.Quote(url)+")")
				_, dataURL, hasMap := strings.Cut(initial.body, "sourceMappingURL=")
				media, encoded, hasData := strings.Cut(dataURL, ",")
				isJSON := media == "data:application/json;base64" || media == "data:application/json;charset=utf-8;base64"
				if !hasMap || !hasData || !isJSON || len(strings.Fields(encoded)) == 0 {
					t.Fatal("generated module has no inline source map")
				}
				mapped, err := base64.StdEncoding.DecodeString(strings.Fields(encoded)[0])
				if err != nil {
					t.Fatal(err)
				}
				var sourceMap struct {
					Sources        []string `json:"sources"`
					SourcesContent []string `json:"sourcesContent"`
					Mappings       string   `json:"mappings"`
				}
				if err := json.Unmarshal(mapped, &sourceMap); err != nil {
					t.Fatal(err)
				}
				if sourceMap.Mappings == "" || !strings.Contains(strings.Join(sourceMap.Sources, "\n"), "original-hot.ts") || !strings.Contains(strings.Join(sourceMap.SourcesContent, "\n"), "ORIGINAL_HOT") {
					t.Fatalf("generated source map lost the original source: %s", mapped)
				}
			}
			wsPath, protocol := "/", "vite-hmr"
			if impl == "oj" {
				wsPath, protocol = "/__ws", ""
			}
			sock, err := hmrsocket.Dial(strings.TrimPrefix(base, "http://"), wsPath, protocol)
			if err != nil {
				t.Fatal(err)
			}
			defer sock.Close()
			write(t, file, `export const value = "`+prefix+`_V2"; if (import.meta.hot) import.meta.hot.accept();`)
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				frame, err := sock.Next(time.Until(deadline))
				if err != nil {
					t.Fatal(err)
				}
				var message struct {
					Updates []struct {
						Path string `json:"path"`
					} `json:"updates"`
				}
				if err := json.Unmarshal([]byte(frame), &message); err != nil {
					t.Fatal(err)
				}
				matched := false
				for _, update := range message.Updates {
					if update.Path != url && !strings.HasSuffix(update.Path, "/hot.js") {
						continue
					}
					if update.Path != url {
						t.Fatalf("generated module HMR URL = %q, want %q", update.Path, url)
					}
					response := get(t, base, update.Path)
					if response.status != 200 {
						t.Fatalf("HMR URL %s returned %d", update.Path, response.status)
					}
					response.contains(t, srv, "GENERATED_V2")
					matched = true
				}
				if matched {
					return
				}
			}
			t.Fatal("no generated module HMR URL was received")
		})
	}

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
		url := "/@fs" + filepath.Join(appBin, "generated", "routes.ts")
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

func restartCount(t *testing.T, s *server) int {
	t.Helper()
	log := s.log(t)
	return strings.Count(log, "[vite-plugin-bazel] restarting:") +
		strings.Count(log, "oj config/env changed — restarting dev server")
}

type devConfig struct {
	Port         int           `json:"port"`
	Host         any           `json:"host"`
	Root         string        `json:"root"`
	FsAllow      []string      `json:"fsAllow"`
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
