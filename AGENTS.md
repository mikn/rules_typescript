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
# `nested-bazel` and `cpu:2`: --config=fast drops them, and a full run bounds
# how many run at once by the machine's cores.
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
           → OxcCompile action (.js + .js.map; + .d.ts under declarations = "oxc")
           → TsgoDeclare action (.d.ts; the default)
             or TsgoCheck validation action (.tscheck stamp in _validation; under "oxc")

.d.ts = compilation boundary. Downstream sees only .d.ts, not .ts source.
Change implementation without changing .d.ts → no downstream recompilation.

TsStrictDeps reads the target's own sources and fails on any specifier no
DIRECT dep provides. Its scanner is the same character walk as Gazelle's
`ScanImports` (gazelle/imports.go) -- a specifier only one of them recognises
is either a dep Gazelle cannot generate or drift nothing notices, so
tests/strict_deps pins the two against one table. Change one, change both.

TsGlobalDts reads the target's .d.ts srcs and writes the reference file naming
the ones public_globals exports -- the file a consumer's tsconfig `files`
lists, since Starlark cannot read a source to tell a global .d.ts from a module
one. A target exporting none runs no such action and provides no such file.
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
- `ts/private/ts_test.bzl` — test macro: vitest by default, `runner = "node:test"` for node's own runner (auto node_modules)
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
- All config via `# gazelle:ts_*` directives. There is no config file.
- Default: every-dir (every directory with .ts files is a package)
- `ts_test` auto-generates node_modules from npm deps in the `deps` list
- Register all new directives in `KnownDirectives()`, all new rules in `Kinds()` + `Loads()`
- Framework bundle targets auto-generated at root when framework detected in `package.json`
- `bazel run //gazelle -- -mode=diff` on a clean tree must print nothing. A
  fixture that differs only in Gazelle's own rendering (a one-element list
  inline, a genrule's output filename over its label) makes real drift
  indistinguishable from formatting, which is what hid it for two rounds. Fix the
  fixture, or pin the hand-written form with `# keep` — `visibility` merges, so
  without `# keep` a hand-narrowed one comes back `//visibility:public` every run

**Testing:**
- Every feature needs a test that ASSERTS correctness (not just "builds without errors")
- Integration tests (`tests/integration/`) test full user journeys in a nested Bazel workspace — create project, gazelle, build, test
- They are part of `bazel test //...`. Tagging a test `manual` takes it out of CI; use `exclusive` when the problem is concurrency, not the test.
- `tests/bootstrap` is deleted. It was a non-hermetic duplicate of `tests/integration` (inherited PATH/HOME/USER, host `bazel`). Do not recreate it.
- Every integration workspace shares ONE repository cache. Each nested Bazel has
  its own output base, so without it all of them fetch the whole BCR registry
  separately and the concurrent lookups fail on a different subset each run —
  which reads as a flaky test rather than as a missing cache. The harness appends
  `common --repository_cache=` to each staged workspace's `.bazelrc`
  (`shareRepositoryCache` in `tests/integration/harness/harness.go`); do not add a
  workspace that bypasses `prepare()`.
- Use `sh_test` for output verification, `go_test` for Gazelle logic, vitest for runtime behavior

**npm:**
- pnpm is hermetic (`bazel run //:pnpm`). No system pnpm needed.
- `--lockfile-only` is the standard for adding packages. No `node_modules/` in source tree.
- npm aliases (e.g., `h3-v2: npm:h3@2.0.1-rc.16`) must produce both the alias and real targets
- Dependency cycles broken by `break_cycles` in `npm/lazy.bzl`: a depth-first walk that drops each edge closing a cycle

## Gazelle Directives (complete list)

| Directive | Effect |
|---|---|
| `ts_package_boundary every-dir\|tsconfig\|true` | Package boundary mode; `true` marks the one directory |
| `ts_declarations tsgo\|oxc` | Choose the declaration emitter for the subtree |
| `ts_path_alias @/ src/` | Path alias (merges with parent) |
| `ts_runtime_dep @npm//:happy-dom` | Always-included test dep |
| `ts_ambient_types @npm//:types_node` | Dep appended to every generated `ts_compile` and `ts_test` in the tree |
| `ts_exclude *.generated.ts` | Exclude pattern: a basename glob, or a `./`-anchored path |
| `ts_exclude_dir coverage` | Directory basename Gazelle does not enter |
| `ts_warn_unresolved true` | Warn on unresolved imports |
| `ts_ignore` | Skip this directory |
| `ts_target_name <name>` | Override target name |
| `ts_codegen <name> <generator> <outs> [srcs:<csv>] [args]` | Custom codegen rule |
| `ts_npm_hub <repo>` | The npm hub bare specifiers in this tree resolve into |
| `ts_npm_mapping <path.json>` | Overlay a hand-written npm name → label mapping on the lockfile inventory |
| `ts_asset_declaration_type <ext> <type>` | The type an asset extension's import resolves to in this tree |
| `ts_js_srcs .mjs .cjs` | Admit JavaScript sources of those extensions into generated `srcs` |

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
`css_module` additionally provides `CssModuleInfo`, whose `exports_files` carry
the `<source>.exports.json` its compile action wrote — the same map the `.d.ts`
keys came from, so anything that needs the runtime values reads that file rather
than deriving names a second time. `ts_bundle`, `ts_dev_server` and `ts_test`
all install `//ts/private/css:css_module_vite_plugin`, which is how that map
reaches the bundler; a fourth consumer must install it too rather than let Vite
scope the stylesheet again.

## npm Internals

ONE REPOSITORY PER PACKAGE is the only implementation. `npm_translate_lock` the
repository rule, and `npm.translate_lock(lazy = ...)`, are deleted — do not
reintroduce a second resolver.

`npm/extensions.bzl` → `npm/lazy.bzl` (whole-graph analysis, no network) →
one `npm_import` per package + one `npm_hub` of aliases.

The analysis stays in the extension because none of it is a decision a package
can make about itself: platform filtering, which package a bare label means
(highest version), `@types` pairing, cycle breaking (`break_cycles`), alias
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
reads is `op \t source \t destination`: `C` copy, `L` directory symlink and `S`
file symlink, copies first so no link is ever dangling.

A resolution is name, version AND peer set: pnpm resolves a package once per
distinct peer set and the outcomes have different dependency edges, so
`NpmPackageInfo.peer_id` carries pnpm's peer suffix (the same token the
snapshot's repository name is built from) and everything keys on it. Two
resolutions of one name declared side by side on ONE target is an error either
way — two versions, or two peer sets of one version — because `node_modules/<name>`
is one directory and Node resolves the bare name to it.

A package's DECLARATION ENTRY POINT is `_exports_types` in
`npm/private/npm_import.bzl`, and the order is a resolver's, not a preference
list. `exports` first, walked in the MAP'S OWN KEY ORDER — Node and TypeScript
try conditions as written, so a fixed priority answers with the wrong build's
declarations for a package that writes `require` before `import` — through array
fallbacks and the conditions-only shorthand; a leaf naming `.js`/`.mjs`/`.cjs`
resolves to the declaration beside it; then top-level `types`, then `typings`,
extensionless form included. `exports` is authoritative about what it designates
and SILENT about the rest: the string-valued `exports["."]` with no `types` key is
where most of npm publishes, every `@types/*` package included, and treating that
silence as an answer is what made a whole closure untyped. Every candidate is
existence-checked against the extracted package, because six `@babel/helper-*`
resolutions in this repo's own lockfile designate a `lib/index.d.ts` their tarball
does not contain. `tests/npm/exports_types_tests.bzl` is the table; add the real
manifest, not a synthesised shape.

The same repository rule reads each designated declaration's triple-slash header
and writes the packages its `/// <reference types=...>` directives name as
`type_references`, because tsgo resolves that directive through typeRoots and
node_modules only -- never `paths` -- and the sandbox has neither. `ts_compile`
and the editor aspect follow the names through the referencing package's own
deps when they put the file in `files`; `tests/npm/type_references_tests.bzl`
pins the header reader and `tests/npm_types_shim` the route.

## The dev server's generated config

Three invariants in `ts/private/ts_dev_server.bzl`, each of which was a silent
no-op before it was one.

**npm resolution is a `node_modules` link, with a plugin behind it, because Vite
has no search-path option.** `resolve.modules` is webpack's; Vite ignores it, so
the line configured nothing and a served source importing a bare specifier
answered 500. Vite resolves a bare specifier by walking up from the importer, or
from `root` for `resolve.dedupe` and `optimizeDeps.include`, and neither walk
goes through the plugin container, so the launcher links the npm tree in as
`<workspace>/node_modules` (`linkAs` in `tools/launcher/plan.go`).
`bazel:npm-resolve` is `enforce: 'post'`, for an importer the walk cannot reach:
it locates `<tree>/<pkg>/package.json`, and if that exists hands the id back to
`this.resolve()` with that manifest as the importer. At `'pre'` it rewrote every
bare importer into the tree, which reads to Vite as a node_modules-internal
import and opts the module out of dependency optimisation. Exports maps,
conditions and subpaths stay Vite's to interpret — do not reimplement any of them
here. A package the tree does not carry returns `null`, so Vite's own
unresolved-import error is what the user sees.

**An entry point comes from the package's `exports` map, never from a path into
its `dist/`.** `@vitejs/plugin-react` moved its entry between the two majors this
repo has built against, and the old fixed `dist/index.mjs` plus a `console.warn`
turned `react_refresh = True` into a no-op that served without Fast Refresh.
`npmEntryPath` reads the manifest; a load failure throws, naming the label and the
dep to add.

**`vite_config` is loaded from a COPY in bin, and that is where the hermeticity
boundary is.** Node resolves a runfiles symlink before it resolves that file's own
imports, so a config loaded from the source tree resolves through a source-tree
`node_modules` this ruleset does not have. A bare npm specifier in the copy
resolves through the `node_modules` tree (which therefore has to be in the same
Bazel package); a relative import does not, because only the one file is copied,
and the server dies naming it rather than starting on half a config.
`//tests/dev_server:vite_config_boundary_test` pins all three sides.

## Framework Support

The mechanism is three parts:
1. `staging_srcs` on `ts_bundle` — copies source files to a writable dir for framework plugin scanning
2. `vite_config` attr — user provides a 3-line `.mjs` with the framework plugin
3. Gazelle auto-generates `node_modules` + `vite_bundler` + `ts_bundle` + `filegroup` targets at the workspace root

**Detecting a framework and being able to bundle it are separate facts, and
`gazelle/framework_bundle.go` has a map for each.** `frameworkConfigs` gets
`ts_bundle` targets; `unsupportedBundling` gets a named log line and no targets.
Only Solid Start is in the second map now — `@solidjs/start` ships no Vite plugin
at all (`defineConfig()` returns a vinxi app, which the `vite_config` contract
cannot consume). A framework in NEITHER map is the one outcome to avoid: no target
and no explanation. If you add a framework, add it to one of the two — or, when
`ts_bundle` genuinely cannot host it, to a generation path of its own.

Two frameworks have such a path, each with a rule that owns a staging root and
runs the framework's own build in it: `next_build` (`generateNextJSBundle`) and
`sveltekit_build` (`generateSvelteKitBundle`, in `gazelle/sveltekit_bundle.go`).
SvelteKit's reason is worth knowing before reaching for `ts_bundle` again: it
resolves `svelte.config.js`, `src/app.html` and the route tree against
`process.cwd()`, and its plugin forces `root: cwd` back over any override, so
`VITE_STAGING_ROOT` — `ts_bundle`'s whole redirection mechanism — is inert for it.
`generateSvelteKitBundle` also suppresses TypeScript targets under `src/`: a BUILD
file there would make a subpackage, and the `glob(["src/**"])` feeding the bundle
does not descend into one. Suppression only stops Gazelle writing one — a BUILD
file already in the tree gets named in the log, and the `sveltekit_build` macro
fails on it via `native.subpackages()`, because a partial route tree still builds
green.

The generated `entry_point` names a single-file `ts_compile` the user declares
(`# gazelle:ts_exclude <entry>` plus the target), because `ts_bundle` needs
exactly one `.js` and Gazelle merges a directory into one target. Until that
target exists the label dangles and `bazel build //...` fails for the whole
workspace — `//tests/integration:remix_test` pins both sides of that.

## Vite and vitest are consumer versions, and one lane is tested

Neither is a ruleset dependency; both come from a consumer lockfile, and the rules
generate config for whatever it resolves to. `MODULE.bazel` translates six
lockfiles into six hubs. Four resolve Vite (`@npm`, `@npm_tailwind`,
`@npm_workers`, `@npm_eslint`), all at 8.2.2, with vitest 4.1.11 wherever a hub
resolves vitest at all, so there is one lane. `@npm` (`tests/npm/pnpm-lock.yaml`)
carries most of it — `tests/vitest/**`, `tests/dev_server/**`,
`tests/vite_bundle/**`, `vite/tests/**` (vite-plugin-bazel's own tests), and the
`lsp`, `npm_deps` and `vite_bundle` integration workspaces, which copy that
lockfile verbatim. `@npm_features` (`tests/npm/pnpm-lock-features.yaml`, declared
`dev_dependency`) is the pnpm patch/alias/peer-variant fixture and resolves
neither tool; `@npm_css` (`ts/private/css/pnpm-lock.yaml`) holds the ruleset's own
build-action packages and resolves neither either. The per-hub table is in
COMPATIBILITY.md § Vite and vitest:

```bash
grep -rnE '^  (vite|vitest)@' --include=pnpm-lock.yaml .
```

A second hub on a second major would not be a second lane either, only a second
lockfile to keep in step: what `@npm_vite` actually bought was `ts_bundle` proven
on Vite 8 while the integration test of the same rule ran on Vite 6. The coupling
it was meant to supply is now a test rather than a constant —
`//vite/tests:peer_version_test` reads `peerDependencies.vite` out of
`vite/package.json` and asserts the installed major is one that range names, so
widening the range is a deliberate edit to both files.

Two known version-sensitive spots in the generated config, both now written the
way the current major wants them: `ts_bundle` emits `output.manualChunks` and an
unnamed `minify` (the plugin `split_chunks` used to emit is gone in Vite 7; naming
`esbuild` picks an optional peer that is not in the tree), and `ts_test`'s
array-`config` form emits `test.projects` — vitest 4 does not deprecate
`test.workspace`, it throws on it.

## Snapshots under Bazel

`ts_test` redirects `test.resolveSnapshotPath` to
`<package>/__snapshots__/<source>.snap` — where a plain `vitest` keeps it — reads
those files from runfiles via the `snapshots` attr, and runs vitest in read-only
snapshot mode (`CI=true`) so no `bazel test` can write a `.snap` and then pass on
what it wrote. Every vitest `ts_test` also declares `<name>.update_snapshots`, which
reuses the test's own `ts_compile` (a second `ts_compile` over the same srcs would
declare the same `.js` outputs) and writes under `BUILD_WORKSPACE_DIRECTORY`.
Update mode pins `test.dir`, `test.include` and `cacheDir`, because `bazel run`
puts the working directory in the user's source tree.

## What NOT to do

- Don't add Python dependencies. All codegen uses awk or Starlark `json.decode()`.
- Don't generate bash scripts for Windows compatibility paths. Use Node.js via the runtime toolchain, or the Go launcher for anything runnable. Runners are Go now; what is left is a few build-action wrappers (the Vite bundler, `next_build`) and the `node_modules` bash fallback. Don't add to that set.
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
- **Gazelle directives > config files.** Directives are visible, inheritable, version-controlled in BUILD files — and they inherit, where a nested config file replaced the list an ancestor had built, so two sites asking "which excludes apply here" got different answers depending on where they asked.
- **`pnpm add --lockfile-only`** is the correct workflow. No `node_modules/` directory should ever exist in the source tree.
- **Two recognisers of one thing drift.** Gazelle's import scanner and the strict-deps checker must agree specifier for specifier, or a hard error becomes unfixable by the tool meant to fix it. Same shape as the `node_modules` tree: the layout planner and the builder read one manifest, not two ideas of it.
- **A name is not a resolution.** Keying anything by npm package name alone (a `node_modules` destination, a patch pairing, a dep edge) loses the version and fails silently, because every version involved is a real version. `name@version` is one key short too: pnpm resolves once per peer set.
- **A green suite is not a preserved suite.** `bazel run //gazelle` once *deleted* hand-written `go_test` targets and still satisfied "builds" and "idempotent" — a deleted test passes both. `bazel query 'tests(//...)'` before and after is the check that catches it, and it is now part of the Gazelle acceptance run.
- **A test that never ran is not a test.** `tests/vitest/environment` was two `manual` targets behind a `build_test`, so no non-default vitest environment had ever executed; the moment one did it failed on runfiles realpathing out of the sandbox. Same for snapshots: `toMatchSnapshot()` asserted nothing at all, because the `.snap` was not in runfiles and vitest treated every run as a first run.
- **Emitting a target that cannot build is worse than emitting none, and silence is worse than both.** Gazelle now names the framework and the reason instead.
- **A config option another tool owns configures nothing.** `resolve.modules` is webpack's; Vite ignored it silently, so the dev server had no npm resolution at all and no test noticed, because no test imported an npm package from served source. A generated option is only real once something reads back the behaviour it was supposed to produce.
- **A `catch` that warns is how a feature becomes a no-op.** `react_refresh = True` reached into `@vitejs/plugin-react/dist/index.mjs`, a filename that major no longer shipped, and served without Fast Refresh behind a `console.warn`. Fail with the label and the fix, or do not catch.
- **The editor has ONE `paths` map, and that is a semantic limit, not a
  detail.** A nested tsconfig extends the root and inherits its map unchanged
  (which is why the root's aliases still resolve from a subdirectory), so
  "resolve this specifier" is a per-target fact on the build and a
  workspace-wide one in the editor. `ts_compile`'s `untyped_packages` is
  per-target; `ts_refresh_tsconfig`'s `host_only_packages` is the workspace-wide
  half, and `check_untyped_agreement` fails when the graph needs both answers
  rather than letting an editor report what a build does not.
- **Silence in a metadata map is not an answer.** `_exports_types` read `exports["."]` and stopped, so a string-valued entry with no `types` key — most of npm, and every `@types/*` package — resolved to nothing and the `paths` entry pointed at a directory. Read what the map designates, then fall through to the fields it is silent about.
- **A real version bump is a test.** Only moving `@npm` to Vite 8 / vitest 4 fired `test.workspace`, the react entry point and the declaration-entry fallback. Two hubs on two majors looked like coverage of exactly that and supplied none of it.
- **esbuild reads the workspace `tsconfig.json`, and that is not hermetic.**
  `srcs` reach the sandbox as symlinks, so esbuild walks up from the entry
  point's REAL path, finds the source-tree `tsconfig.json`, and applies its
  `paths` — which `//:refresh_tsconfig` fills with `.bazel/npm/**` `.d.ts`
  files. A bundled npm package then resolved to a declaration file. Every
  `esbuild_bundle` passes `--tsconfig-raw={}`; the only reason nothing noticed
  earlier is that the vite plugin's single import is `--external`.
- **Formatting drift hides real drift.** For two rounds `bazel run //gazelle` could not be applied here, because ten fixtures differed from Gazelle's own rendering and nobody could tell those files from the ones it was actually changing. Keep the clean-tree diff empty so the next non-empty one means something.
- **Every hub name is the consumer's to claim.** `npm/extensions.bzl` gives the
  root module's `translate_lock` priority for *any* hub name, so none of them is
  privileged: a ruleset-internal target naming `@npm//:x` resolves into whatever
  lockfile the consumer registered, and the `dev_dependency` hubs do not exist
  for a consumer at all. `//vite:esbuild_node_modules` named `@npm//:esbuild`,
  and it feeds `//vite:vite_plugin_bazel` — which `ts_dev_server` takes through
  its `plugin` attr, and which Gazelle writes when it *generates* a dev server
  (it leaves an existing one alone). `plugin` has no default, and no workspace
  here set it, so nothing had reached the label and no build had failed: three
  of this repo's six examples have no esbuild in their lockfile and would have.
  Reachability, not correctness, is what decided that — the `@npm` labels still
  in `//vite` and `//ts/private/css` are unreached rather than sanctioned. What
  pins the rule is two trees, `//vite:esbuild_node_modules` and
  `//ts/private/css:node_modules`, declared in `tests/npm/BUILD.bazel`.
