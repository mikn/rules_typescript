# AGENTS.md — rules_typescript

Instructions for AI agents and contributors working on this codebase.

**This file is a living document.** When you discover important patterns, preferences, or lessons about working on this project, add them here. Keep it terse.

## Quality Standard

This ruleset targets **rules_go ergonomic parity**. The bar: a TypeScript developer writes `.ts` files, runs `bazel run //:gazelle`, then `bazel build //...` and `bazel test //...` — everything works with zero manual BUILD file editing.

## Contribution Workflow

**Always use the PR workflow. Never push directly to main.**

```bash
# 1. Create a branch
git checkout -b feat/my-feature

# 2. File an issue first (for non-trivial changes)
gh issue create --title "..." --body "..."

# 3. Develop using the three-stage cycle (see below)

# 4. Create a PR
gh pr create --title "feat: ..." --body "Fixes #N"

# 5. After review + CI green, merge
gh pr merge --squash
```

**Issue tracking:**
```bash
gh issue create --title "..." --body "..." --label "enhancement"
gh issue create --title "..." --body "..." --label "bug"
gh issue list
```

## Development Workflow

```bash
bazel test //...                           # everything; `bazel query 'tests(//...)' | wc -l` for the count
bazel test --config=fast //...             # skips the nested-Bazel integration tests
bazel build //... --output_groups=+_validation  # redundant if .bazelrc has it
cd e2e/basic && bazel test //...           # e2e workspace (in .bazelignore)
cd examples/react-app && bazel test //...  # example workspace (in .bazelignore)

# One integration test on its own. Each spawns a nested Bazel and is tagged
# `exclusive`, so they run serially rather than being excluded.
bazel test //tests/integration:new_project_test
```

## Three-Stage Development Cycle

For any non-trivial change:

1. **Implement** — write code, build, test, iterate until green
2. **Adversarial review** — separate agent finds bugs, design flaws, shell injection, depset violations
3. **Fix** — address all CRITICAL and HIGH findings, verify

Do not skip the review stage. It has caught real bugs in every round.

## Architecture (terse)

```
ts_compile → TsStrictDeps action (.strictdeps stamp; gates the compile)
           → OxcCompile action (.js + .d.ts + .js.map)
           → TsgoCheck validation action (.tscheck stamp in _validation)

.d.ts = compilation boundary. Downstream sees only .d.ts, not .ts source.
Change implementation without changing .d.ts → no downstream recompilation.

TsStrictDeps reads the target's own sources and fails on any specifier no
DIRECT dep provides. Its scanner is the same character walk as Gazelle's
`ScanImports` (gazelle/imports.go) -- a specifier only one of them recognises
is either a dep Gazelle cannot generate or drift nothing notices, so
tests/strict_deps pins the two against one table. Change one, change both.
```

**Key files:**
- `ts/defs.bzl` — public API (all rules, providers, macros)
- `ts/private/ts_compile.bzl` — core compilation rule
- `ts/private/providers.bzl` — JsInfo, TsDeclarationInfo, BundlerInfo, CssInfo, AssetInfo, NpmPackageInfo
- `npm/private/npm_translate_lock.bzl` — pnpm lockfile reader (parsing only; no repository rule)
- `npm/extensions.bzl` — the `npm` module extension (translate_lock, pnpm tags)
- `npm/lazy.bzl` — whole-graph analysis + one `npm_import` per package + the alias hub
- `npm/private/npm_import.bzl` — the per-package repository rule and `npm_hub`
- `ts/private/pnpm.bzl` — hermetic pnpm download + `ts_pnpm`/`ts_add_package` macros
- `ts/private/ts_config.bzl` — the public `ts_config` rule (a hand-written tsconfig.json and its `extends` chain)
- `platforms/platforms.bzl` — the one platform table (`PLATFORMS`) everything loads
- `ts/toolchain/BUILD.bazel` — toolchain types and instances; `//ts/toolchain:all`
- `ts/private/ts_test.bzl` — vitest test macro (auto node_modules)
- `ts/private/ts_bundle.bzl` — Vite production bundler (staging_srcs for frameworks)
- `ts/private/ts_dev_server.bzl` — dev server with HMR
- `ts/private/ts_codegen.bzl` — general code generation
- `ts/private/tsconfig_aspect.bzl` — the IDE tsconfig, the hook data, and the
  aspect that writes per-target fragments
- `tools/launcher/` — the one Go launcher `ts_binary`, `ts_test`,
  `ts_dev_server` and `npm_bin` run through; `--dump-config` prints the
  resolved per-target JSON config
- `gazelle/generate.go` — BUILD file generation
- `gazelle/resolve.go` — import → label resolution
- `gazelle/config.go` — directives, framework detection, codegen detection
- `gazelle/framework_bundle.go` — auto-generated framework bundle targets
- `gazelle/codegen.go` — auto-detected codegen targets
- `oxc_cli/src/main.rs` — Rust CLI (parse → isolated_declarations → transform → codegen)
- `vite/bundler.bzl` — Vite bundler wrapper

## Rules (never break these)

**Starlark:**
- Never materialize depsets at analysis time (no `.to_list()` in rule impls unless unavoidable + commented)
- `depset(order = "postorder")` for all transitive file sets
- `ctx.actions.run` only. `ctx.actions.run_shell` is gone from the ruleset and
  nothing new may reintroduce it
- Shell strings: always use `_shell_escape()` for any interpolated path
- All `fail()` calls must have actionable messages with "Did you mean...?" suggestions
- No Python dependencies. Use awk or Starlark `json.decode()`.

**Bazel:**
- bzlmod only. No WORKSPACE.
- Never reference `bazel-out/` directly. Use `ctx.bin_dir.path`, `File.path`.
- Optional toolchains: `config_common.toolchain_type(TYPE, mandatory = False)`
- Validation actions in `OutputGroupInfo(_validation = ...)`, not separate targets
- No `bazel clean`. Iterate. Trust the cache.
- Consumer toolchain registration is explicit: `register_toolchains("@rules_typescript//ts/toolchain:all")`

**Gazelle (Go):**
- All config via `# gazelle:ts_*` directives (not `gazelle_ts.json`, which is deprecated)
- Default: every-dir (every directory with .ts files is a package)
- `ts_test` auto-generates node_modules from npm deps in the `deps` list
- Register all new directives in `KnownDirectives()`, all new rules in `Kinds()` + `Loads()`
- Framework bundle targets auto-generated at root when framework detected in `package.json`

**Testing:**
- Every feature needs a test that ASSERTS correctness (not just "builds without errors")
- Integration tests (`tests/integration/`) test full user journeys in a nested Bazel workspace — create project, gazelle, build, test
- They are part of `bazel test //...`. Tagging a test `manual` takes it out of CI; use `exclusive` when the problem is concurrency, not the test.
- `tests/bootstrap` is deleted. It was a non-hermetic duplicate of `tests/integration` (inherited PATH/HOME/USER, host `bazel`). Do not recreate it.
- Use `sh_test` for output verification, `go_test` for Gazelle logic, vitest for runtime behavior

**npm:**
- pnpm is hermetic (`bazel run //:pnpm`). No system pnpm needed.
- `--lockfile-only` is the standard for adding packages. No `node_modules/` in source tree.
- npm aliases (e.g., `h3-v2: npm:h3@2.0.1-rc.16`) must produce both the alias and real targets
- Dependency cycles broken via Kosaraju's SCC algorithm

## Gazelle Directives (complete list)

| Directive | Effect |
|---|---|
| `ts_package_boundary every-dir\|index-only\|true` | Package boundary mode |
| `ts_declarations tsgo\|oxc` | Choose the declaration emitter for the subtree |
| `ts_path_alias @/ src/` | Path alias (merges with parent) |
| `ts_runtime_dep @npm//:happy-dom` | Always-included test dep |
| `ts_exclude *.generated.ts` | Exclude pattern |
| `ts_warn_unresolved true` | Warn on unresolved imports |
| `ts_ignore` | Skip this directory |
| `ts_target_name <name>` | Override target name |
| `ts_codegen <name> <generator> <outs> [args]` | Custom codegen rule |

## Provider Contract

Every `ts_compile` target provides: `JsInfo` + `TsDeclarationInfo` +
`TsModuleInfo` + `OutputGroupInfo(_validation)`. `_validation` is only populated
under `declarations = "oxc"`; under the default the declarations are the proof.
A `ts_compile` with any `deps` additionally exposes the strict-deps stamp: in
`OutputGroupInfo(strict_deps = ...)` always, and as an input to the compile
actions so a violation fails the build rather than only `--output_groups`.
Every `ts_npm_package` provides: `JsInfo` + `TsDeclarationInfo` +
`NpmPackageInfo` (whose `direct_deps` carries the per-dependent resolution the
`node_modules` links are built from).
`css_library`/`css_module`/`asset_library`/`json_library` provide `TsDeclarationInfo` (for .d.ts stubs).

## npm Internals

ONE REPOSITORY PER PACKAGE is the only implementation. `npm_translate_lock` the
repository rule, and `npm.translate_lock(lazy = ...)`, are deleted — do not
reintroduce a second resolver.

`npm/extensions.bzl` → `npm/lazy.bzl` (whole-graph analysis, no network) →
one `npm_import` per package + one `npm_hub` of aliases.

The analysis stays in the extension because none of it is a decision a package
can make about itself: platform filtering, which package a bare label means
(highest version), `@types` pairing, cycle breaking (Kosaraju's SCC), alias
naming, patch routing. Each package then reads its own `package.json` and writes
its own BUILD file, which is what makes on-demand fetching possible.

Handled: scoped packages, `@types` pairing, multiple versions with
version-suffixed labels, bin scripts (fixed `:bin` alias per package, since the
hub cannot know whether a bin exists without downloading), conditional exports,
pnpm workspaces and `workspace:*` links, npm aliases (their own labels),
dependency cycles, `patchedDependencies` (verified four ways at extension time:
a label resolving to no readable file, a file whose sha256 disagrees with the
lockfile digest, a declared patch with no label, and a file no entry claims).

No code needed, pinned by tests: catalogs, overrides (including `parent>child`),
packageExtensions. pnpm resolves all three at every use site.

`node_modules` trees are flat where flat is unambiguous and keyed by RESOLUTION
where it is not: a name's primary resolution keeps the top-level directory, every
other one gets its bytes once under
`.pnpm/<name>@<version>[_<peer set>]/node_modules/<name>`, and each dependent
that resolved to one of those gets a relative symlink. The manifest the builder
reads is `op \t source \t destination`, `C` copy and `L` symlink, copies first so
no link is ever dangling.

A resolution is name, version AND peer set: pnpm resolves a package once per
distinct peer set and the outcomes have different dependency edges, so
`NpmPackageInfo.peer_id` carries pnpm's peer suffix (the same token the
snapshot's repository name is built from) and everything keys on it. Two
resolutions of one name declared side by side on ONE target is an error either
way — two versions, or two peer sets of one version — because `node_modules/<name>`
is one directory and Node resolves the bare name to it.

## Framework Support

The mechanism is three parts:
1. `staging_srcs` on `ts_bundle` — copies source files to a writable dir for framework plugin scanning
2. `vite_config` attr — user provides a 3-line `.mjs` with the framework plugin
3. Gazelle auto-generates `node_modules` + `vite_bundler` + `ts_bundle` + `filegroup` targets at the workspace root

**Detecting a framework and being able to bundle it are separate facts, and
`gazelle/framework_bundle.go` has a map for each.** `frameworkConfigs` gets bundle
targets; `unsupportedBundling` gets a named log line and no targets. SvelteKit and
Solid Start are in the second map — SvelteKit's plugin runs its own sync step from
the Vite `config` hook and its `.svelte` routes are not TypeScript, and
`@solidjs/start` ships no Vite plugin at all (`defineConfig()` returns a vinxi
app, which the `vite_config` contract cannot consume). A framework in NEITHER map
is the one outcome to avoid: no target and no explanation. If you add a framework,
add it to one of the two.

The generated `entry_point` names a single-file `ts_compile` the user declares
(`# gazelle:ts_exclude <entry>` plus the target), because `ts_bundle` needs
exactly one `.js` and Gazelle merges a directory into one target. Until that
target exists the label dangles and `bazel build //...` fails for the whole
workspace — `//tests/integration:remix_test` pins both sides of that.

## Vite and vitest are consumer versions, and two lanes are tested

Neither is a ruleset dependency; both come from a consumer lockfile, and the rules
generate config for whatever it resolves to. Two hubs are exercised on purpose,
because a generated config that only ever meets one generation breaks silently on
the next:

- `@npm` (`tests/npm/pnpm-lock.yaml`) — Vite 6, vitest 3. `tests/vitest/**`,
  `tests/dev_server/**`, the integration workspaces, `examples/`.
- `@npm_vite` (`vite/pnpm-lock.yaml`) — Vite 8, vitest 4. `tests/vite_bundle/**`,
  and `vite/tests/**` (vite-plugin-bazel's own tests, including a `ts_test` on
  vitest 4).

Do not collapse them onto one lockfile. Two known version-sensitive spots:
`ts_bundle` emits `output.manualChunks` and an unnamed `minify` (the plugin
`split_chunks` used to emit is gone in Vite 7; naming `esbuild` picks an optional
peer that is not in the tree), and `ts_test`'s array-`config` form still emits
`test.workspace`, which vitest 4 removed and throws on.

## Snapshots under Bazel

`ts_test` redirects `test.resolveSnapshotPath` to
`<package>/__snapshots__/<source>.snap` — where a plain `vitest` keeps it — reads
those files from runfiles via the `snapshots` attr, and runs vitest in read-only
snapshot mode (`CI=true`) so no `bazel test` can write a `.snap` and then pass on
what it wrote. Every `ts_test` also declares `<name>.update_snapshots`, which
reuses the test's own `ts_compile` (a second `ts_compile` over the same srcs would
declare the same `.js` outputs) and writes under `BUILD_WORKSPACE_DIRECTORY`.
Update mode pins `test.dir`, `test.include` and `cacheDir`, because `bazel run`
puts the working directory in the user's source tree.

## What NOT to do

- Don't add Python dependencies. All codegen uses awk or Starlark `json.decode()`.
- Don't generate bash scripts for Windows compatibility paths. Use Node.js via the runtime toolchain, or the Go launcher for anything runnable. Runners are Go now; what is left is a few build-action wrappers (the Vite bundler, `next_build`) and the `node_modules` bash fallback. Don't add to that set.
- Don't add `gazelle_ts.json` features. Use directives.
- Don't create separate `_check` targets. Use `_validation` output group on the compile target.
- Don't assume `@npm` is the only repo name. Support custom names via the npm extension.
- Don't push directly to main. Use PRs.
- Don't skip the integration tests when adding new features, and don't tag them `manual` to make a run faster.

## Lessons Learned (add to this section)

- **End-to-end tests catch real bugs.** The nested-Bazel journey tests found 5 bugs on their first run, including a Rust binary bug where oxc-bazel ignored the isolated-declarations flag. They also spent a release tagged `manual`, which is how "34 pass" came to read as full coverage of a 54-target suite.
- **Shell escaping is never optional.** Every path interpolated into a shell string must use `_shell_escape()`. Three separate review rounds caught injection vectors.
- **npm alias support is non-obvious.** pnpm's `"h3-v2": "npm:h3@2.0.1-rc.16"` pattern requires both the alias name AND real name as `ts_npm_package` targets with different `package_name` values.
- **Framework Vite plugins need writable filesystems.** `staging_srcs` solves this by copying source files to a temp dir inside the Bazel action. General mechanism, not framework-specific.
- **`bazel clean` is never the answer.** If the build is broken, the bug is in the rules, not the cache. Fix the root cause.
- **Every `fail()` should tell the user what to do.** "Did you mean...?" suggestions prevent hours of debugging.
- **Gazelle directives > config files.** `gazelle_ts.json` was a mistake. Directives are visible, inheritable, version-controlled in BUILD files.
- **`pnpm add --lockfile-only`** is the correct workflow. No `node_modules/` directory should ever exist in the source tree.
- **Two recognisers of one thing drift.** Gazelle's import scanner and the strict-deps checker must agree specifier for specifier, or a hard error becomes unfixable by the tool meant to fix it. Same shape as the `node_modules` tree: the layout planner and the builder read one manifest, not two ideas of it.
- **A name is not a resolution.** Keying anything by npm package name alone (a `node_modules` destination, a patch pairing, a dep edge) loses the version and fails silently, because every version involved is a real version. `name@version` is one key short too: pnpm resolves once per peer set.
- **A green suite is not a preserved suite.** `bazel run //gazelle` once *deleted* hand-written `go_test` targets and still satisfied "builds" and "idempotent" — a deleted test passes both. `bazel query 'tests(//...)'` before and after is the check that catches it, and it is now part of the Gazelle acceptance run.
- **A test that never ran is not a test.** `tests/vitest/environment` was two `manual` targets behind a `build_test`, so no non-default vitest environment had ever executed; the moment one did it failed on runfiles realpathing out of the sandbox. Same for snapshots: `toMatchSnapshot()` asserted nothing at all, because the `.snap` was not in runfiles and vitest treated every run as a first run.
- **Emitting a target that cannot build is worse than emitting none, and silence is worse than both.** Gazelle now names the framework and the reason instead.
