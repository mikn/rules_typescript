# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/).

Nothing here has been released. There is no git tag, no GitHub release and no
Bazel Central Registry entry; `0.1.0` is the version string in `MODULE.bazel`,
not a shipped artifact. Consumers pin a commit
([quickstart](https://mikn.github.io/rules_typescript/getting-started/quickstart/#depending-on-rules_typescript)),
so every entry below is a change against whatever commit you pinned last.

Pre-1.0, breaking changes land without a deprecation path. The breaking
sections are the migration guide: every break is listed with the edit it
requires.

## [Unreleased]

### Breaking — `ts_compile`

- **New hard error: an import no *direct* dep provides fails the build.** A
  `TsStrictDeps` action reads the target's own sources and reports every
  specifier that resolves only because some dep's own deps happen to carry it,
  naming the file, the line, the specifier and the label to add:

  ```
  //src/app:app imports a module no direct dep provides:

    src/app/main.ts:1  imports "./hidden"
                       add "//src/app:hidden" to deps
  ```

  The transitive closure is still an action input, so a declared dep's own
  `.d.ts` keeps resolving its own imports — what changed is that arriving
  transitively no longer *satisfies* an import. Relative paths, bare
  specifiers, npm packages and `module_name` targets are all checked; node
  builtins, `node:` specifiers and `path_aliases` prefixes are exempt, and an
  import nothing in the closure provides is left to TypeScript's `TS2307`,
  since there is no label to suggest. There is no flag and no opt-out. Run
  `bazel run //:gazelle` to fix a failure, or add the printed labels by hand.
  `/// <reference types="x" />` is deliberately not checked — Gazelle does not
  generate a dep for it either, and the two recognisers stay in agreement.
- `isolated_declarations = True|False` is replaced by
  `declarations = "tsgo"|"oxc"`, default `"tsgo"`. `"tsgo"` emits `.d.ts` from
  the full type program, so no export needs an explicit type annotation and a
  type error fails a plain `bazel build`. `"oxc"` is the old behaviour: a
  syntactic per-file emit that requires isolated declarations, with
  type-checking demoted to a `_validation` action. Gazelle's
  `# gazelle:ts_isolated_declarations <bool>` becomes
  `# gazelle:ts_declarations <tsgo|oxc>`.
- `ts_compile_legacy` is deleted, not ported. It left oxc as the emitter with
  isolated declarations off, which silently widened declarations
  (`export declare const PATTERNS: {}` for an object of five `RegExp`s) with no
  diagnostic anywhere. Move those targets to the `declarations` default.
- Under `declarations = "tsgo"`, `enable_check = False` now means no type
  program and therefore **no `.d.ts` output at all**. It is the right setting
  for terminal targets (app entries, dev servers, bundle inputs) and wrong for
  anything another `ts_compile` depends on.
- New attributes: `tsconfig`, `lib`, `types`, `jsx_import_source`,
  `compiler_options`, `module_name`. See
  [ts_compile](https://mikn.github.io/rules_typescript/rules/ts-compile/#where-compiler-options-come-from) for
  the precedence rules.
- The generated tsconfig no longer sets `strict`, `module`,
  `moduleResolution`, `skipLibCheck` and `esModuleInterop` unconditionally.
  With `tsconfig` set they come from your file (or from tsc's defaults if it
  omits them); without it the previous baseline still applies. A target that
  relied on being strict-checked while extending a non-strict tsconfig now
  checks under the file's options instead.
- `target` and `jsx_mode` are still injected in every mode and supersede a
  `target`/`jsx` in the tsconfig file, because oxc transforms with them and the
  two compilers must agree.
- **New hard analysis error:** `compiler_options` naming any of the 15
  Bazel-owned keys (`paths`, `baseUrl`, `rootDir`, `rootDirs`, `outDir`,
  `declarationDir`, `declaration`, `declarationMap`, `emitDeclarationOnly`,
  `noEmit`, `noEmitOnError`, `isolatedDeclarations`, `composite`,
  `incremental`, `tsBuildInfoFile`) fails analysis and names the attribute to
  use instead. Such a value would previously have been applied and broken the
  sandbox layout.
- **New hard analysis error:** a `path_aliases` value pointing into
  `bazel-out/` or `bazel-bin/` fails analysis. That path embeds the build
  configuration, so it breaks under `-c opt` or a different exec platform. To
  import another target by bare specifier, set `module_name` on the target that
  produces the declarations and depend on it.

### Breaking — new public API

- `ts_config` rule (`//ts:defs.bzl`). Declares a hand-written `tsconfig.json`
  plus the files it `extends`, since Starlark cannot read the file to follow the
  chain. A tsconfig that extends nothing can be passed to `ts_compile` directly
  without it.
- `TsModuleInfo` provider (`//ts:defs.bzl`). Carries the bare specifier a
  target is importable as (`module_name`) and the directories its declarations
  land in, so a consumer's tsconfig `paths` no longer has to name output paths
  by hand.

### Breaking — `ts_test`

- A vitest config is now **always** generated and always passed with
  `--config`, so vitest never auto-discovers a stray config from the runfiles
  tree.
- `config` **merges** into the generated config instead of replacing it, and
  accepts an inline dict as well as a label. Objects merge key by key, arrays
  concatenate base-first (matching vite's `mergeConfig`), later scalars win. A
  `config` that previously replaced the Bazel layer wholesale now composes with
  it.
- New attributes: `setup_files`, `global_setup`, `data`, `globals`,
  `reporters`, `coverage_thresholds`.
- `environment` is emitted into the config instead of being passed on the
  command line, and is no longer validated against a fixed set of names.
- The CSS-module mock now actually takes effect. Class names from
  `*.module.css` imports change from vite's hashed form to the plain property
  name — the behaviour the docs always described. **Expect snapshot and
  assertion updates in tests that touched CSS-module class names.**
- The auto-generated `node_modules` tree moved to a per-target directory, so a
  package may now hold more than one `ts_test`.
- The config that actually ran is readable:
  `bazel build //path:my_test --output_groups=vitest_config`.
- **Snapshots work, and a stale one now fails.** Vitest resolves a `.snap` beside
  the test file it ran, which under Bazel is the compiled `.js` in `bazel-out` —
  never the source tree. So a `bazel test` **passed against a deliberately stale
  checked-in snapshot**: the `.snap` was not in runfiles, vitest treated it as
  new, wrote it into the sandbox, and reported success. `toMatchSnapshot()` under
  `ts_test` asserted nothing at all. Three changes together fix that:
    - A fourth, root-only config layer sets `test.resolveSnapshotPath` to
      `<package>/__snapshots__/<source name>.snap` — where a plain `vitest` run
      already keeps it, so an adopting repository renames nothing.
    - A new `snapshots` attr (`glob(["__snapshots__/*.snap"])`) puts those files
      in runfiles, which is what makes a stale or absent one a failure.
    - `CI=true` is set on every non-update run, vitest's read-only snapshot mode,
      so no `bazel test` can write a `.snap` and pass on what it just wrote. It
      goes through `dict.setdefault`, so `env = {"CI": "false"}` still opts out.
      Its only other effect is `allowOnly` → `false`.
- **Every `ts_test` now also declares `<name>.update_snapshots`**, an executable
  that runs the same compiled tests with `--update` and writes under
  `BUILD_WORKSPACE_DIRECTORY`. It reuses the test's own `ts_compile`, which is
  what removes the output collision the previously-documented recipe hit: two
  `ts_test`-like targets over the same srcs in one package are two `ts_compile`s
  declaring the same `.js` and `.d.ts`. `update_snapshots = True` survives as a
  standalone updater, now documented as unable to share a package with a test
  over the same srcs. **`--sandbox_writable_path` is no longer part of any
  snapshot workflow.**
- **A DOM `environment` runs under sandboxing.** `resolve.preserveSymlinks: true`
  is in the Bazel layer: a browser-like environment realpaths every module id,
  which for a runfiles symlink resolves to the execroot path *outside* the test
  sandbox (`Failed to load url … Does the file exist?`). The node environment
  never hit it, because its ssr transform loads the path it is given.

### Breaking — npm

- One external repository per npm package is the only implementation. The
  `npm_translate_lock` repository rule (~1770 lines) and
  `npm.translate_lock(lazy = ...)` are **deleted**;
  `npm/private/npm_translate_lock.bzl` is now only the pnpm lockfile reader. The
  label surface does not move: `@npm//:zod`, `@npm//:types_react`,
  `@npm//:vitest_bin` all still resolve, through an alias hub that downloads
  nothing.
- `npm.translate_lock` gains `patches`, a label list. Previously
  `patchedDependencies` was ignored outright and the unpatched upstream tarball
  was installed with no warning. Files are matched to lockfile entries by
  pnpm's own `pnpm patch-commit` name,
  `<name with / replaced by __>@<version>.patch`, and every pairing is
  **verified at extension time**, so the check does not depend on the patched
  package entering some build's closure. Four failures, each naming the label:
  a label that resolves to no readable file; a file whose sha256 disagrees with
  the digest `patchedDependencies` records; a `patchedDependencies` entry with
  no matching label; and a passed file no entry claims. A patch filename
  starting with `@` cannot be exported by `exports_files(glob(["*.patch"]))` —
  `glob()` prefixes `:` onto such a result and `exports_files` rejects it as a
  target name, failing the whole package — so the unreadable-label message says
  to list those files literally.
- npm alias specifiers (`h3-v2: h3@2.0.1`) get their own labels. They
  previously collapsed onto the real package's label, so the name the importing
  code uses did not exist as a target.
- `npm_bin`'s `optional_dep_info` attribute is removed — only the deleted
  single-repository path produced it. Platform-matched `optionalDependencies`
  (native sidecars such as `oxlint` → `@oxlint/linux-x64-gnu`) are now named
  through `optional_dep_packages`.
- pnpm `catalogs`, `overrides` (including the `parent>child` form) and
  `packageExtensions` need no support code and got none: pnpm resolves all
  three at every use site, so the lockfile already carries concrete versions and
  injected peers. Tests pin that, so a parser change cannot start reading
  `specifier:` instead of `version:` unnoticed.

### Breaking — `node_modules` trees

- A tree now places **every** resolved version of a package. One npm name can
  resolve to more than one version in a single closure, and the old tree keyed
  every destination by package name alone: both versions wrote to
  `node_modules/<name>`, the last copy won, and every dependent silently got
  that version. Now each name's primary version keeps the flat top-level
  directory (primary = what the tree's own `deps` declare, else the highest
  version — the rule `@npm//:<name>` already follows), every other version gets
  its bytes once under `.pnpm/<name>@<version>/node_modules/<name>`, and each
  dependent that resolved to one of those gets a relative symlink at
  `<dependent>/node_modules/<name>`. Nothing that resolved before moves.
- **New hard error:** declaring two versions of one name directly on a single
  `node_modules` target. `node_modules/<name>` is one directory, so there is no
  arrangement that answers `import "<name>"` with both; the failure names the
  target, both versions, and the two ways out (depend on one and let the other
  arrive transitively, or split into two targets).
- `NpmPackageInfo` gains `direct_deps`, the per-dependent resolution the links
  are built from. `ts_test`'s auto-generated tree gets the same layout — it
  goes through the same builder.
- **A tree is keyed by *resolution*, not by `name@version`.** `name@version` was
  one key short of pnpm's: pnpm resolves a package once per distinct set of
  peers, and those outcomes share a tarball but have different `dependencies:`
  maps. `ansi-styles@6.2.3(ansi-regex@5.0.1)` and
  `ansi-styles@6.2.3(ansi-regex@6.2.2)` used to collapse onto one directory, and
  the resolution a target *declared* was as likely to be the discarded one as a
  transitively-reached one. `NpmPackageInfo` gains `peer_id`, carrying pnpm's
  peer suffix — the same token the snapshot's repository name is built from, so
  there is one mangler rather than two — and the store path becomes
  `.pnpm/<name>@<version>_<peer set>/node_modules/<name>`. The peer component is
  a readable prefix plus a digest of the whole set: a nested peer set can run to
  hundreds of characters, and truncating alone would collide two resolutions into
  one directory. A tree with no peer-differing edge is unchanged byte for byte; a
  tree with one gains the files of the resolution that was previously dropped.
- **New hard error:** declaring two peer resolutions of one version on a single
  `node_modules` target — the version error, one level narrower. `import
  "<name>"` from the tree root answers with one directory, and *that directory's
  own* `node_modules/<name>` is one directory too, which is exactly what the two
  variants disagree about. The message distinguishes it from the version case.
- `ts_test` no longer keeps its own npm-package collector. The duplicate keyed by
  `name@version`, so it dropped a peer variant before the layout planner ever saw
  it — meaning `ts_test` trees could not have been fixed from `node_modules.bzl`
  alone. Both paths now call one `collect_npm_packages`.

### Breaking — toolchains and repositories

- The `ts.oxc_toolchain()`, `ts.tsgo_toolchain()` and `ts.node()` extension
  tags are removed. `ts.tsgo(version = ...)` is the only remaining tag.
- `rules_typescript_dependencies()` is gone, and with it `ts/repositories.bzl`.
  The `ts` extension declares its own repositories.
- The `@oxc` repository is gone — oxc resolves directly to
  `//oxc_cli:oxc-bazel`, built from source by `rules_rust` for the exec
  platform. `@tsgo` is replaced by per-platform `@tsgo_<platform>`
  repositories.
- `PLATFORM_CONSTRAINTS` is replaced by `//platforms:platforms.bzl%PLATFORMS`.
  The new `//platforms` package is the single platform vocabulary and also
  declares `//platforms:<key>` (for `--platforms`) and `//platforms:is_<key>`
  (for `select()`).
- oxc and tsgo are now `exec_compatible_with`-constrained instead of
  `target_compatible_with`-constrained, so they follow the execution platform.
  A build that set `--platforms` to anything unusual previously lost its
  compilers.
- New toolchain type `//ts/toolchain:js_tool_type` separates
  node-as-a-build-tool (exec config) from node-as-a-runtime
  (`js_runtime_type`, whose `runtime_binary` is now `cfg = "target"`).
  Registering `@rules_typescript//ts/toolchain:all` picks up both.

### Breaking — the IDE tsconfig

- A target's `module_name` now gets its own `compilerOptions.paths` entry, so a
  first-party package imported by bare specifier resolves in the editor. The
  `module_name` keys are emitted last, so a first-party name beats a same-named
  npm package — the precedence the tsconfig `ts_compile` generates already
  used.
- `ts_refresh_tsconfig` gains `extra_exclude`, globs appended to the generated
  `exclude`. TypeScript trees that are not in this module's build graph — a
  nested Bazel module, an example workspace in `.bazelignore` — are otherwise
  walked by `tsc` and checked under the wrong `compilerOptions`, because
  nothing in the graph names them and `include` is `**/*`.
- The `ts_compile` targets `ts_test` generates from `srcs`, `setup_files` and
  `global_setup` follow the test's `visibility`, defaulting to
  `//visibility:public`, instead of a hardcoded `//visibility:private` that no
  BUILD file could widen. They were unnameable, so `ts_refresh_tsconfig(deps =
  ...)` could not reach the npm packages only a test declares. A consumer who
  wants them locked down sets `visibility` on the `ts_test`.

### Breaking — bundling, assets and packaging

- `ts_bundle` requires `bundler`. The bundler-less mode concatenated every
  transitive `.js` file behind a `// Placeholder bundle` header and called it a
  bundle — a plausible-looking artifact no runtime accepts. Point the attr at a
  `vite_bundler` (or any `BundlerInfo` provider). `ts_binary` is unchanged:
  without a bundler it runs the entry point `.js` directly, which is a real
  thing to do.
- `css_module` and `json_library` need the `js_tool` toolchain, and
  `ts_npm_publish` and `next_build` now do too — `register_toolchains("@rules_typescript//ts/toolchain:all")`
  covers all of them. `css_module` and `ts_npm_publish` used to shell out to the
  host `awk`.
- `css_module` extracts class names by parsing the stylesheet rather than
  regex-matching `.name` anywhere in the file. Declaration values
  (`url(logo.png)` no longer declares `png`), strings, at-rule preludes,
  `@keyframes` bodies and `:global(...)` groups contribute no names, and a name
  that is not a bare TypeScript identifier is emitted quoted
  (`readonly "button-primary": string`) instead of as a syntax error.
- `ts_npm_publish` stages into `<name>_pkg/package/` instead of `<name>_pkg/`,
  and its `package.json` is rewritten through `JSON.parse`/`JSON.stringify`, so
  a template that is not one-field-per-line survives. Output is pretty-printed
  two-space JSON.
- `ts_npm_package` requires `package_dir` and `package_files`; the impl always
  dereferenced both.
- **`ts_bundle`'s generated Vite config no longer names anything Vite removed or
  made optional.** `split_chunks` emits
  `build.rollupOptions.output.manualChunks` rather than `splitVendorChunkPlugin`,
  which Vite 7 removed — a plain `import` of a name that no longer exists, and
  the reason the old spelling survived is that the ruleset only ever tested one
  Vite generation. `minify = True` emits `true`, meaning the running Vite's own
  default minifier (esbuild on 6, oxc on 8), rather than naming `"esbuild"`:
  Vite 8 accepts the name but then installs `buildEsbuildPlugin()`, and esbuild
  is an *optional* peer, absent from a tree built from `deps = ["@npm//:vite"]`.
  And `minify = False` now also emits `output.minify: false`, because
  `build.minify: false` alone still runs the bundler's dead-code pass, which
  re-emits each chunk from its AST and silently discards whatever a plugin's
  `renderChunk` returned — string form and `{code}` form both.

### Breaking — tests

- `tests/bootstrap` is deleted, all 8 targets. Every scenario it covered
  already exists under `tests/integration/` with three more besides, and the
  bootstrap copies were the non-hermetic ones: they inherited `PATH`/`HOME`/`USER`
  and found `bazel` on the caller's PATH. Use
  `bazel test //tests/integration/...`. The `RULES_TYPESCRIPT_ROOT` environment
  variable those runners needed is no longer read anywhere.
- Integration targets are `exclusive` rather than `manual`, so
  `bazel test //...` now runs them — currently 158 test targets, 13 of them
  `exclusive` and 2 `manual`
  (`bazel query 'tests(//...)' | wc -l` if that has moved).
  `bazel test --config=fast //...` skips the exclusive ones.

### Breaking — the dev server

- `bazel run //app:dev` serves **first-party source**. Vite transforms the
  checked-in `.ts` in memory, so a keystroke reaches the browser with no Bazel
  analysis-and-action cycle in between; `bazel-bin` stays authoritative only
  for what Vite cannot produce itself — the npm tree, `ts_codegen` output,
  assets and passthrough `.d.ts`. Generated code is recognised by having no
  checked-in source, not by a path list. The dev server previously served the
  compiled `.js` from `bazel-bin`, which put a Bazel rebuild in the inner loop.
- **The dev server no longer type-checks.** That is native Vite parity — Vite
  has never type-checked — but it makes editor correctness load-bearing: a type
  error now surfaces in the editor and in `bazel build`, and no longer blocks
  the browser update. `ts_bundle` is unchanged: a production bundle still
  consumes Bazel's compiled output.
- The restart decision moved into the Vite process. `ibazel run` SIGTERMs the
  launcher after every rebuild and the launcher deliberately survives it, so
  one Vite process lives across rebuilds; a `ConfigWatcher` compares content
  digests of the inputs the generated config was built from (the config itself,
  the npm tree, the toolchain node binary) and restarts only when one of those
  changes. A `ts_codegen` rebuild no longer restarts the server, and neither
  does a source edit.

### Breaking — shell replaced by Go

- `scripts/` is deleted. `ci.sh` was a sequence of bazel invocations that
  duplicated `.github/workflows/ci.yml`; `quickstart.sh` and `release.sh` are
  `bazel run //tools/quickstart` and `//tools/release`; `verify_determinism.sh`
  is explicit CI steps, because two builds cannot be one action.
- `ts_binary`, `ts_test`, `ts_dev_server` and `npm_bin` run through one
  checked-in Go launcher that reads a per-target JSON config. The generated
  file is `<target>_launcher`, **not** `<target>_runner.sh`; anything naming the
  old file breaks. `--dump-config` prints the resolved config.
- `tools/refresh_tsconfig.sh` is deleted. `ts_refresh_tsconfig` takes `deps`
  and an aspect walks them — a `deps = []` call produces a tsconfig with no
  `paths` at all. Its `execroot_symlink` attr is removed.
- `npm_import`'s `expected_prefix` attr is removed; the prefix is detected.

### Added

- `ts_compile` can consume a real `tsconfig.json`: the generated config
  `extends` it in place rather than copying it, so relative paths inside it
  still resolve. This unblocks ambient-globals-only packages
  (`types: ["./worker-configuration.d.ts"]`), `jsx: preserve` with
  `jsxImportSource`, `lib: ["webworker"]`, `resolveJsonModule` and
  `allowImportingTsExtensions`.
- npm packages are fetched on demand. A target's npm cost is its own dependency
  closure rather than the whole lockfile, repositories fetch in parallel,
  caching and invalidation are per package, and a malformed tarball fails only
  its own package.
- `//tests/lsp:test_tsserver_diagnostics` passes for the first time. It wanted
  a `typescript` module from a host `node_modules` that does not exist on a
  clean machine, so it had never run anywhere; `typescript` now comes from the
  lockfile through `@npm`.

- `//tests/integration/npmrc_registry` proves `.npmrc` private registries end
  to end: a throwaway local registry that 401s unauthenticated requests, so a
  scoped `registry=` override and an `_authToken` reaching the wire as
  `Authorization: Bearer` are both asserted, not just parsed.
- A root `go.mod` and `go.work`, so `go vet`, `staticcheck` and `gopls` reach
  the ruleset's Go. They could not before, and `go vet` immediately found a
  redeclaration Bazel cannot see: three files in one directory each declared
  `const placeholder`, which compiles because each is its own single-src
  `go_test`. CI now gates on `gofmt -l` and `go vet`.
- All six `examples/` workspaces are in the CI matrix. It built two, so
  `examples/tanstack-app` was broken with nobody watching.
- **A second npm hub, `@npm_vite`, built from `vite-plugin-bazel`'s own
  lockfile.** The bundle rules are now exercised against Vite 8 / vitest 4
  (`tests/vite_bundle`, `vite/tests`) while `ts_test`, `ts_dev_server` and the
  integration workspaces stay on Vite 6 / vitest 3 (`@npm`,
  `tests/npm/pnpm-lock.yaml`). Two generations on purpose: a generated config
  that only ever meets one is a config that breaks silently on the next, which is
  how `splitVendorChunkPlugin` survived a passing test. The plugin's peer range
  and the Vite it is tested against now share one lockfile. See
  [COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#vite-and-vitest).
- `//tests/integration:remix_test` — a nested-Bazel journey through a real Remix
  workspace: Gazelle, then `bazel build` on what it wrote, then assertions on
  the produced artifacts (`client/index.html` with hashed refs,
  `client/.vite/manifest.json`, one hashed chunk per route). It also pins the two
  attrs that are load-bearing rather than decorative, by asserting the *right*
  failure without each: dropping `package.json` from `staging_srcs` fails on
  `_staging/package.json`, and a root-relative `entry_point` fails
  `bazel build //...` on `'//:entry_client' does not exist`.
- `//tests/vitest/snapshot` and `//tests/vitest/environment:{dom,node}_test` —
  the first tests in the repository to actually run a snapshot assertion and a
  non-default vitest environment. The environment pair asserts the same claim
  from both sides (one needs a `document`, the other needs there to be none), so
  an `environment` that never reached vitest fails one of them whichever way it
  defaulted. `jsdom` and `edge-runtime` stay analysis-only behind a `build_test`,
  which is enough for what they pin: that the attr is not a fixed list.

### Fixed

- An `@types/*` dep supplies its ambient globals. The generated tsconfig used to
  derive `typeRoots` from the dirname of each `@types` declaration, which under
  one-repository-per-package named the package directory itself and `external/`
  -- neither of which is a directory whose *children* are type packages, so
  TypeScript resolved nothing and `process`, `setTimeout`, `Buffer` and
  `import 'node:fs'` were all errors. The entry-point `.d.ts` of each direct
  `@types/*` dep is now listed in the generated tsconfig's `files`, and no
  `typeRoots` is derived at all. `NpmPackageInfo` gains `ambient_types_file`;
  `TsDeclarationInfo.type_roots` is deleted.
- Consumer setup no longer asks for
  `build --@rules_rust//rust/toolchain/channel=stable`. The flag is a
  consumer-visible error (`No repository visible as '@rules_rust'`) and a no-op
  for this ruleset, whose Rust channel already defaults to stable.
- Install instructions document the pre-BCR `git_override` /
  `archive_override` / `local_path_override` recipes, since there is no
  registry entry or release for a bare `bazel_dep` to resolve against.
- Cargo build output is gitignored; `oxc_cli/target/` had 2322 tracked files,
  622 MiB of blobs, which made a source tarball of HEAD 207 MB instead of
  638 KB.
- Gazelle parses `tsconfig.json` as JSONC — comments and trailing commas no
  longer make `compilerOptions.paths` silently disappear.
- Gazelle names `css_library`, `css_module`, `asset_library` and `json_library`
  targets after the whole filename (`button.css` → `button_css`), so they no
  longer collide with the directory-named `ts_compile` target or with each
  other.
- Integration runners no longer default their scratch directory to `/tmp`: a
  nested Bazel output base does not fit in a small tmpfs, and the failure
  surfaced as "No space left on device" from cgo, nowhere near its cause.
- The tsgo repository rule no longer runs an unchecked host `chmod +x`; the
  exec bit comes from the npm tarball.
- `ts_npm_publish` no longer creates an undeclared `package` symlink next to
  its output: two publish targets in one package wrote that same path, so a
  concurrent build could archive the wrong directory. The staging directory is
  itself named `package` and `tar -C` reads it directly.
- `ts_npm_publish`'s staging action runs under `set -euo pipefail`; a failed
  copy used to leave a partial package and exit 0.
- `ts_binary` with a Vite bundler no longer fails analysis with "No attribute
  'env_vars' in attr". The Vite config generator reads the bundle attrs
  `ts_binary` does not declare through their defaults.
- Rules that run Node inside a build action (`css_module`, `json_library`,
  `ts_codegen`, `next_build`, `ts_npm_publish`, `vite_bundler`) resolve it from
  the `js_tool` (exec platform) toolchain instead of `js_runtime` (target
  platform). Under a `--platforms` that differs from the host, they used to
  build actions that ran the target's Node — `node.exe` on a Linux builder.
- `vite_bundler` and `next_build` no longer fall back to a `node` from `PATH`
  when no toolchain is registered; the missing toolchain is an error.
- `//vite:vite_plugin_bazel` is built by a rule that resolves the Node
  toolchain, replacing a genrule whose four hand-written `config_setting`s
  keyed the *target* platform to pick an exec-platform `@nodejs_*` binary. It
  failed analysis outright under `--platforms=//platforms:windows_amd64`.

- An npm tarball's top-level directory is detected with
  `rctx.path().readdir()` instead of shelling out to the host `tar`. The old
  code returned `""` when `tar` failed and fell back to `package`, which aborts
  the fetch for DefinitelyTyped-style tarballs — `@types/express-serve-static-core`
  packs under `express-serve-static-core v4.19/`, the case the function exists
  for. No `rctx.execute` call remains in the ruleset.
- Launchers resolve paths through the Bazel runfiles library, so they work with
  a runfiles **manifest** and no symlink tree. The hand-rolled
  `RUNFILES_DIR`/`TEST_SRCDIR`/`$0.runfiles` discovery died at `cd` in that
  layout. Four hand-written shell-quoting helpers are deleted with it.
- Cycle breaking removes only cycle-closing edges. Self-edges are now removed
  (a one-member SCC broke nothing, so Bazel saw the cycle), and an edge between
  two *distinct* cycles is kept instead of dropped. On this repo's own lockfile
  it drops 2 edges where the old code dropped 4.
- `ts_dev_server` no longer falls back to a host-PATH `node` or `vite`. A
  missing JS runtime toolchain fails at analysis time; a missing
  `node_modules`/vite fails the launcher with an actionable message.
- The Vite HMR watcher rides Vite's own `server.watcher` instead of
  `import('chokidar')`. chokidar is inlined into Vite's dist chunk and is not an
  installed package, so the import always threw `ERR_MODULE_NOT_FOUND` and every
  rebuild was dropped silently. A watcher that cannot start now warns.
- The Vite bundler creates a `node_modules` beside the generated config.
  `linux-sandbox` remounts `/` read-only, so Vite's `.vite-temp` mkdir hit the
  source tree with `EROFS` — which Node's `recursive: true` mkdir masks as
  `ENOENT`, and Vite only tolerates `EACCES`. `examples/tanstack-app` could not
  build.
- Gazelle names a framework bundle's node_modules tree `node_modules`. Bazel
  materialises a tree artifact as per-file symlinks into the real execroot, and
  Node realpaths a module before resolving its bare imports, so a tree named
  `<app>_node_modules` made every Vite framework bundle fail to find `rolldown`.
- No action needs a shell on the exec platform: `ctx.actions.run_shell` is gone
  from the ruleset.
- Gazelle's path-alias resolution is deterministic. When several
  `compilerOptions.paths` entries match one specifier — a tsconfig declaring
  both `@x` and `@x/*` — the longest matching alias key wins, which is what
  TypeScript's own resolution does (an exact pattern is necessarily the longest
  key that can match). It previously took whichever key Go's randomised map
  iteration yielded first, so two identical Gazelle runs could disagree and one
  of the answers was a dep label that does not exist. Ties break
  lexicographically, and reading `compilerOptions.paths` is sorted too, so a
  colliding pattern pair resolves the same way every run.
- An alias key without a trailing slash now matches only at a path-segment
  boundary: `@shared` no longer swallows `@sharedX`. That makes Gazelle's
  "is this specifier an alias" answer identical to the one the strict-deps
  check uses, so the generator and the check cannot disagree about a
  specifier's category.
- Gazelle's import scanner is a character-walk lexer rather than a set of
  regexes, and it is the same walk the strict-deps check runs. It previously
  missed `import def, * as ns from "x"` entirely (no dep generated at all) and
  matched specifiers inside template literals (deps on labels that do not
  exist).
- **Gazelle resolves a specifier that spells out its extension.** The index is
  built with extensions dropped, and the lookup used the path as written, so a
  NodeNext-style `./rules/foo.js` over a `foo.ts` source asked for a key that was
  never in the index — and then invented `//<dir>/foo.js` as the dep, a label that
  does not exist. One candidate list now serves both sides: the path as written,
  the path with its extension dropped, that stem under each known extension, and
  `<stem>/index.ts[x]`. The same drift the strict-deps checker already avoided by
  stripping; Gazelle did not.
- **Gazelle no longer writes a dep for a Node built-in spelled without
  `node:`.** `import { join } from "path"` became `@npm//:path`, which does not
  exist. The exemption now matches the strict-deps checker's on the bare name.
- **A `ts_compile`'s `module_name` is indexed, and a bare specifier consults the
  index before npm.** `import "@acme/lib"` became `@npm//:acme_lib`; the hub has
  no such package, so the label did not exist.
- **A generated `ts_compile` carries only the path aliases it can satisfy.** The
  whole workspace `paths` map was copied onto every generated target, and
  `ts_compile` hard-fails on an alias whose files are none of its inputs, so a
  Gazelle run could leave dozens of targets failing analysis. A target now gets
  the aliases its own imports match, plus any alias whose directory holds its own
  sources — which is exactly the set `ts_compile` accepts, mirrored from its
  validation so a generated target cannot trip it. Aliases read back out of a
  tsconfig this ruleset generated are an echo, not a declaration, and are
  skipped; a `# gazelle:ts_path_alias` directive is a declaration and reaches the
  IDE tsconfig's `paths` even when nothing imports through it yet.
- **A generated target no longer claims a hand-written target's srcs.** Two
  `ts_compile`s over one source declare the same `.js` and `.d.ts`, which is a
  conflicting-action error rather than a duplicate-target one. Generation now
  drops any src an existing `ts_compile`/`ts_test`/`css_*`/`asset_library`/
  `json_library` in the same BUILD file already lists, plus any `ts_test`'s
  `setup_files` and `global_setup`.
- **Gazelle no longer emits a dep on the importing package itself.** A module in
  the importer's own package that no rule claims used to resolve to that
  package's own label — a dependency cycle. It now resolves to nothing, and an
  unindexed module elsewhere resolves to its *directory*'s target when the last
  segment names a file.
- **Gazelle names the framework it will not bundle.** SvelteKit and Solid Start
  were emitting `node_modules` + `vite_bundler` + `ts_bundle` targets that cannot
  build — SvelteKit's plugin runs its own sync step from the Vite `config` hook
  and wants files `staging_srcs` cannot carry, and `@solidjs/start` ships no Vite
  plugin at all (`defineConfig()` returns a vinxi app, which the `vite_config`
  contract silently discards, so the bundle built *green* with zero framework
  involvement). Both are still detected; both now get a log line naming the
  framework, the reason, and the fallback, and no target. A framework recognised
  by neither map is the outcome this is designed to prevent: no target and no
  explanation.
- **The generated Remix bundle builds.** Its `entry_point` was `":entry_client"`
  — root-package-relative, and the root has no TypeScript — so the label dangled
  and killed `bazel build //...` for the whole workspace, not just that target.
  It now names `//app:entry_client`, and `package.json` is staged, which the
  `@remix-run/dev` plugin reads from the staging root.
- `bazel run //path:my_test.update_snapshots` no longer writes a `.vite/` cache
  directory into the source tree: update mode runs with the working directory at
  `BUILD_WORKSPACE_DIRECTORY`, from which vite derived `cacheDir`, so the layer
  pins `cacheDir` under the target's output directory instead.

### Measurements

Two numbers are quoted in the docs. Both come from one machine on one day, and
only one of them can still be reproduced from this tree; the npm entry adds a
query anyone can run against their own lockfile.

- Declaration emitters, via `tools/bench_declarations.sh 20 50 3` (1,000
  annotated files, 20 packages, one linear chain, medians of three interleaved
  runs): `declarations = "tsgo"` 6.3s wall / 4.89s critical path,
  `declarations = "oxc"` 3.8s / 2.15s, `"oxc"` with `enable_check = False`
  2.7s / 1.06s. The script is committed; re-run it on your own graph.
- npm layout, measured while both layouts still existed: building one vitest
  target from an empty output base against a 2731-package lockfile went from
  392s / 2.9 GB of `external/` to 66s / 415 MB, fetching 138 packages
  (vitest's actual transitive closure) instead of all 2731. The
  single-repository arm has since been deleted, so this is a historical record,
  not something a reader can re-run. What *is* reproducible is the shape of it:
  `bazel query 'kind(ts_npm_package, deps(//your:test))'` counts the package
  targets a target can reach, and on this repository's own lockfile a vitest
  test target reaches 123 of them, in 121 repositories.

### Known gaps

Recorded rather than hidden.

- **Windows is unsupported**, not partially supported. See
  [COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#platforms).
- Ambient globals reach a target from its **direct** `@types/*` deps only.
  TypeScript with a real `node_modules` also picks up transitively installed
  ones; here declaring the dep is how a target asks for them.
- `skipLibCheck: true` masks ambient-vs-lib conflicts, so what bites a Workers
  package is a lib declaration winning over an ambient one at the use site.
  Unsettled.
- `jsdom` and `edge-runtime` are still analysis-only: neither is in a lockfile
  the build reads, so those two `environment` values are pinned by a
  `build_test` rather than by a run. `happy-dom` and `node` do run.
- **`ts_test` still emits `test.workspace`**, which vitest 3.2 renamed `projects`
  and vitest 4 removed outright — it throws rather than ignoring it. So the one
  `config` shape that produces it (a file default-exporting an array) is vitest 3
  only. Not currently red: `tests/vitest/**` runs 3.0.9, and every other `config`
  shape is proven on 4.1.11 through `//vite/tests:peer_version_test`.
- `coverage_thresholds` reaches the generated config, but its enforcement is
  unproven.
- **A `vite_config` that is a source file is not hermetic.** The generated config
  imports it by exec-root path; Node realpaths that back into the source tree
  before resolving the config's own bare imports, so the framework plugin is only
  found through a source-tree `node_modules` — which this project forbids and no
  CI job exercises. A *generated* `vite_config` under `bazel-out` works, because
  it sits beside the hermetic tree. `ts_bundle` should stage the user's config
  the way it stages `staging_srcs`.
- **`npm_import`'s `_exports_types` has no fallback**, so a package whose
  `exports["."]` is a plain string and which has no top-level `types` gets a
  `paths` entry pointing at a directory TypeScript cannot resolve. Vite 8 is
  exactly that shape, which is why `//vite:plugin_typecheck` still typechecks
  against Vite 6's `.d.ts` while `vite/package.json` declares
  `peerDependencies.vite: ^8.0.0`.
- **`ts_dev_server` has no npm resolution path of its own.** `resolve.modules` is
  not a Vite option in any version, so that line is dead config: a bare
  `import "react"` from a dev-served source resolves only if the consumer happens
  to have a real root `node_modules`. Relatedly, `react_refresh` imports
  `@vitejs/plugin-react/dist/index.mjs` by fixed path; the pinned 4.4.1 has that
  file, and 6.1.0 (the first major peering on Vite 8) does not, at which point
  the `catch` turns `react_refresh = True` into a silent no-op.
- **Two Vite majors cannot coexist in one Bazel package.** After the Vite
  bundler wrapper's `ln -sf`, Node realpaths through the symlink, so the upward
  walk starts inside the real tree and can reach a *sibling* target's
  `node_modules` in the same package. A `node_modules()` target not named
  `node_modules` therefore resolves through whichever sibling is.
- Gazelle's Node-builtin list is hand-maintained while the strict-deps checker
  uses `builtinModules`. Both reduce to the bare name, so `fs/promises` agrees;
  a name Node exposes only under `node:` (`node:sqlite`, `node:test`) would have
  Gazelle write a label that does not exist.
- `_short_digest` is Java's `String.hashCode` masked to 32 bits. Two peer
  suffixes agreeing on their first 40 sanitised characters *and* colliding on
  that hash merge into one repository, and now also one store directory. The
  bound pre-dates resolution keying (same token) and is not eliminated; a real
  hash function is not available in Starlark.
- The `node_modules` tree action resolves node through `js_runtime_type` (the
  target platform) rather than `js_tool_type` (the exec platform). Every other
  build action was moved; this one was not. Harmless while exec == target,
  wrong under cross-compilation.
- No libc (glibc vs musl) `constraint_setting`. Nothing would reference it
  until npm's platform `select()` lands.
- The strict-deps check does not read `/// <reference types="x" />`. It is a
  real resolution channel, but Gazelle generates no dep for it, so checking it
  would produce failures Gazelle cannot fix.
- `eslint-plugin/**` and `tools/isolated-declarations-lint/**` have no buildable
  targets, and the blocker is a lockfile rather than Gazelle: every source there
  imports `@typescript-eslint/utils`, which no lockfile the build reads contains,
  and with strict deps a hard error nothing over those files can compile. Both
  trees carry a `# gazelle:ts_ignore` naming exactly that. Behind it waits a
  genuine package-level cycle between `eslint-plugin/src` and `src/rules`, which
  one-target-per-directory cannot express.
- The IDE `tsconfig.json` has a single `compilerOptions` block, so targets that
  disagree about `strict`, `allowJs` or `lib` cannot all be correct in it. The
  generated `paths` are a coverage mechanism, not a per-target compiler-option
  mechanism; a target whose own options differ is checked correctly by
  `bazel build` and approximately by the editor. Sources that belong to no
  `ts_compile` target are not in the program's `paths` at all.

## [0.1.0] — never released

No tag, no release and no registry entry exists for this version. The list
below records what the ruleset did before the changes above, so a consumer
pinned to an older commit can tell what moved.

- Core TypeScript compilation with oxc-bazel (Rust-based, per-file transform)
- tsgo (Go port of TypeScript) emits `.d.ts` from the full type program by
  default, so unmodified TypeScript compiles with no explicit export
  annotations, and type errors fail `bazel build` because the declarations are
  real outputs
- `declarations = "oxc"` as an opt-in throughput mode: Oxc emits `.d.ts`
  syntactically (requiring isolated declarations, which it enforces) and
  type-checking becomes a validation action off the critical path
- npm dependency management via pnpm-lock.yaml parser (v6 and v9 formats)
- Multiple npm package version support with semver-correct alias resolution
- npm bin script generation (`@npm//:vitest_bin`, `@npm//:esbuild_bin`, …)
- Conditional exports (package.json `exports` field) resolution
- pnpm workspace support (`workspace:*` protocol)
- Dependency cycle detection and breaking (Kosaraju's SCC algorithm)
- Gazelle extension for BUILD file auto-generation
- Gazelle every-dir default (every directory with `.ts` files is a package)
- Gazelle directives: `ts_package_boundary`, `ts_declarations`, `ts_path_alias`,
  `ts_runtime_dep`, `ts_exclude`, `ts_warn_unresolved`, `ts_ignore`,
  `ts_target_name`, `ts_codegen`
- Gazelle auto-detection of TanStack Router, Prisma, GraphQL codegen, OpenAPI
  generators
- Gazelle reads `tsconfig.json` `compilerOptions.paths` automatically
- Gazelle generates `ts_compile`, `ts_test`, `ts_lint`, `ts_dev_server`,
  `css_library`, `css_module`, `asset_library`, `json_library`, `ts_codegen`
  targets
- `ts_test` with vitest (auto node_modules from deps, DOM testing, coverage,
  snapshots, custom config, environment selection)
- `ts_binary` (runnable, entry_file convention, index.js default)
- `ts_bundle` with real Vite integration (ESM/CJS, tree-shaking, minification,
  chunk splitting, source maps, app mode, env_vars)
- `ts_dev_server` with ibazel HMR support and React Fast Refresh
- Vite plugin injection via `vite_config` attr (unlocks Remix, SvelteKit,
  TanStack Start)
- `ts_codegen` rule for code generation (TanStack routes, Prisma, GraphQL,
  OpenAPI, custom)
- `css_library`, `css_module` (typed `.d.ts` from regex extraction),
  `asset_library`, `json_library` (fully typed `.d.ts`)
- `ts_lint` rule (ESLint/oxlint as validation action)
- `ts_npm_publish` rule (tarball with auto-filled main/types/exports)
- ESLint plugin for isolated declarations migration
  (`require-explicit-types` rule)
- `vite_types` attr on `ts_compile` for `import.meta.env` and asset URL types
- `path_aliases` attr on `ts_compile` for tsgo type-checking with path aliases
- JS runtime toolchain (Node.js via rules_nodejs, pluggable for
  Deno/Bun/workerd)
- `toolchain_utils` (v1.3.0) integration for resolved toolchain targets
- Linux ARM64 platform support (oxc built from source, tsgo from npm)
- IDE support via `bazel run //:refresh_tsconfig` and a live tsserver
  resolution hook
- GitHub Actions CI workflow
- Examples: basic, app (zod), react-app (React + testing-library),
  tanstack-app (TanStack Router SPA)
- BCR presubmit configuration
