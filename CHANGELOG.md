# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/).

Nothing here has been released. There is no git tag, no GitHub release and no
Bazel Central Registry entry; `0.2.0` is the version string in `MODULE.bazel`.
Consumers pin a commit
([quickstart](https://mikn.github.io/rules_typescript/getting-started/quickstart/#depending-on-rules_typescript)),
so every entry below is a change against the commit you pinned last.

Pre-1.0, breaking changes land without a deprecation path. The breaking
sections list every break with the edit it requires.

## [0.2.0]

### Breaking — `ts_compile`

- **An import no direct dep provides now fails the build.** A `TsStrictDeps`
  action reads the target's own sources and reports every specifier that
  resolves only because a dep's own deps carry it:

  ```
  //src/app:app imports a module no direct dep provides:

    src/app/main.ts:1  imports "./hidden"
                       add "//src/app:hidden" to deps
  ```

  The transitive closure is still an action input, so a declared dep's own
  `.d.ts` keeps resolving its own imports; arriving transitively no longer
  satisfies an import. Relative paths, bare specifiers, npm packages and
  `module_name` targets are all checked. Node builtins,
  `node:` specifiers and `path_aliases` prefixes are exempt. An import nothing
  in the closure provides is left to TypeScript's `TS2307`, since there is no
  label to suggest. There is no flag and no opt-out. Run `bazel run //:gazelle`
  to fix a failure, or add the printed labels by hand.
  `/// <reference types="x" />` is not checked, and Gazelle generates no dep for
  it either.
- `isolated_declarations = True|False` is replaced by
  `declarations = "tsgo"|"oxc"`, default `"tsgo"`. `"tsgo"` emits `.d.ts` from
  the full type program: no export needs an explicit type annotation, and a
  type error fails a plain `bazel build`. `"oxc"` is the old behaviour, a
  syntactic per-file emit requiring isolated declarations, with type-checking
  demoted to a `_validation` action. Gazelle's
  `# gazelle:ts_isolated_declarations <bool>` becomes
  `# gazelle:ts_declarations <tsgo|oxc>`.
- `ts_compile_legacy` is deleted. It left oxc as the emitter with isolated
  declarations off, which silently widened declarations
  (`export declare const PATTERNS: {}` for an object of five `RegExp`s). Move
  those targets to the `declarations` default.
- Under `declarations = "tsgo"`, `enable_check = False` means no type program
  and therefore **no `.d.ts` output at all**. Use it for terminal targets (app
  entries, dev servers, bundle inputs), not for anything another `ts_compile`
  depends on.
- New attributes: `tsconfig`, `lib`, `types`, `jsx_import_source`,
  `compiler_options`, `module_name`. See
  [ts_compile](https://mikn.github.io/rules_typescript/rules/ts-compile/#where-compiler-options-come-from)
  for the precedence rules.
- **`tsconfig` layers, it does not replace.** `strict`, `module: "Preserve"`,
  `skipLibCheck` and `esModuleInterop` are applied in **both** modes. Without a
  `tsconfig` they go into the generated config as before. With one they go into a
  `<target>.tsconfig_baseline.json` the generated config `extends` **first**, so
  every key your file — or its own `extends` chain — mentions wins, and the
  baseline reaches only the keys it says nothing about. A target extending a
  non-strict tsconfig still checks under that file's options; a target whose
  tsconfig omits one of them now keeps the ruleset's value instead of falling
  back to tsc's default.
  **Edit required** only if you were relying on a `tsconfig` to un-set one of
  them: say so in the file (`"strict": false`) and it wins, or override it
  in `compiler_options`, which sits above both.
- **`moduleResolution: "Bundler"` is stated only where the ruleset also owns
  `module`.** TypeScript couples the two, and layers merge per key, so a
  baseline resolver left standing under your `"module": "NodeNext"` was `TS5109`
  at line 2 of a generated file — 25 targets in one repo, none of which had
  anything wrong with them. It is now in the generated config without a
  `tsconfig` (and only while `compiler_options` names neither key), and in
  neither file with one. Nothing that resolved before resolves differently:
  tsgo derives `Bundler` from every `module` but `Node16`/`NodeNext`, which
  derive their own. A `tsconfig` naming `module: "Node16"`/`"NodeNext"`, or
  `"CommonJS"`, now compiles instead of failing on the pair.
- **New hard analysis error:** `compiler_options` setting `moduleResolution` to
  `Node16` or `NodeNext` with no `module` beside it, and no `tsconfig` that
  could carry one, fails analysis naming the value to set. That pair is `TS5110`
  whatever the ruleset defaults `module` to, so it is reported with the label
  rather than against a generated file.
- `target` and `jsx_mode` are still injected in every mode and supersede a
  `target`/`jsx` in the tsconfig file, since oxc transforms with them.
- **New hard analysis error:** `compiler_options` naming any of the 16
  Bazel-owned keys (`paths`, `baseUrl`, `rootDir`, `rootDirs`, `outDir`,
  `declarationDir`, `declaration`, `declarationMap`, `sourceMap`,
  `emitDeclarationOnly`, `noEmit`, `noEmitOnError`, `isolatedDeclarations`,
  `composite`, `incremental`, `tsBuildInfoFile`) fails analysis and names the
  attribute to use.
- **New hard analysis error:** a `path_aliases` value pointing into
  `bazel-out/` or `bazel-bin/` fails analysis; that path embeds the build
  configuration, so it breaks under `-c opt` or a different exec platform. To
  import another target by bare specifier, set `module_name` on the target that
  produces the declarations and depend on it.
- **A dep's global `.d.ts` is now in your program.** A `.d.ts` in a target's
  `srcs` with no top-level import or export declares globals, and those names
  are now in scope in every target that depends on it, however far down the
  graph the declaration sits — the scope a single `tsc` run over the same
  sources gives them. A `TsGlobalDts` action classifies each `.d.ts` and writes
  the reference file a consumer's tsconfig `files` lists. Names that used to be
  a `TS2304` now resolve, and an `interface` a dep declares merges with one of
  the same name you declare yourself, so a value that satisfied your `Env` can
  fail with `TS2741` once a dep's `Env` adds a member:

  ```
  src/app/main.ts(1,14): error TS2741: Property 'B' is missing in type
  '{ A: string; }' but required in type 'Env'.
  ```

  Drop the edge if the two `Env`s were never the same type, or rename one of
  them. Declaration-internal collisions stay silent: the generated tsconfig
  keeps `skipLibCheck` on.

### Breaking — new public API

- `ts_config` rule (`//ts:defs.bzl`). Declares a hand-written `tsconfig.json`
  plus the files it `extends`, which Starlark cannot read the file to find. A
  tsconfig that extends nothing goes to `ts_compile` directly.
- `TsModuleInfo` provider (`//ts:defs.bzl`). Carries the bare specifier a
  target is importable as (`module_name`) and the directories its declarations
  land in, so a consumer's tsconfig `paths` no longer names output paths by
  hand.

### Breaking — `ts_test`

- A vitest config is now **always** generated and always passed with
  `--config`, so vitest never auto-discovers a stray config from the runfiles
  tree.
- `config` **merges** into the generated config and accepts an inline dict as
  well as a label. Objects merge key by key, arrays concatenate base-first
  (matching vite's `mergeConfig`), later scalars win.
- New attributes: `setup_files`, `global_setup`, `data`, `globals`,
  `reporters`, `coverage_thresholds`, `tsconfig`. `tsconfig` is forwarded to the
  `ts_compile` the macro generates; the rule already had the attribute, only the
  macro did not pass it along.
- `environment` is emitted into the config, no longer passed on the command
  line, and is no longer validated against a fixed set of names.
- **A `*.module.css` import resolves to the real export map.** The Bazel
  layer's plugin reads `<source>.exports.json`, the map `css_module` wrote and
  generated the `.d.ts` from, so `styles.button` is the scoped string a bundler
  emits (`_button_<8 hex>`) and a test can assert on a rendered `class`
  attribute. It was a `Proxy` returning the property name, so
  `expect(styles.button).toBe("button")` asserted about a string no browser ever
  sees. **Expect snapshot and assertion updates in tests that touched CSS-module
  class names.** A `*.module.css` with no `css_module` target behind it keeps
  the proxy.
- The auto-generated `node_modules` tree moved to a per-target directory, so a
  package may now hold more than one `ts_test`.
- The config that actually ran is readable:
  `bazel build //path:my_test --output_groups=vitest_config`.
- **Snapshots work, and a stale one now fails.** Vitest resolves a `.snap`
  beside the test file it ran, which under Bazel is the compiled `.js` in
  `bazel-out`, never the source tree, so a `bazel test` passed against a stale
  checked-in snapshot: vitest treated the absent `.snap` as new, wrote it into
  the sandbox and reported success, so `toMatchSnapshot()` under `ts_test`
  asserted nothing. Three changes fix that:
    - A fourth, root-only config layer sets `test.resolveSnapshotPath` to
      `<package>/__snapshots__/<source name>.snap`, where a plain `vitest` run
      already keeps it, so an adopting repository renames nothing.
    - A new `snapshots` attr (`glob(["__snapshots__/*.snap"])`) puts those
      files in runfiles, which is what makes a stale or absent one a failure.
    - `CI=true` is set on every non-update run, vitest's read-only snapshot
      mode. It goes through `dict.setdefault`, so `env = {"CI": "false"}` still
      opts out. Its only other effect is `allowOnly` → `false`.
- **Every `ts_test` now also declares `<name>.update_snapshots`**, an
  executable that runs the same compiled tests with `--update` and writes under
  `BUILD_WORKSPACE_DIRECTORY`. It reuses the test's own `ts_compile`, which
  removes an output collision: two `ts_test`-like targets over the same srcs in
  one package are two `ts_compile`s declaring the same `.js` and `.d.ts`.
  `update_snapshots = True`
  survives as a standalone updater, now documented as unable to share a package
  with a test over the same srcs. **`--sandbox_writable_path` is no longer part
  of any snapshot workflow.**
- **A DOM `environment` runs under sandboxing.** `resolve.preserveSymlinks:
  true` is in the Bazel layer. A browser-like environment realpaths every
  module id, which for a runfiles symlink resolves to the execroot path outside
  the test sandbox (`Failed to load url … Does the file exist?`).
- **A `config` that default-exports an array emits `test.projects`, not
  `test.workspace`.** vitest 4 throws on `test.workspace`, so that shape was a
  startup crash on 4. The array form now needs vitest 3.2 or later, the release
  that renamed the key.
  Object, function, promise and inline-dict shapes run on 3 and 4 alike.

### Breaking — Vite and vitest versions

- **The tested lane is Vite 8 / vitest 4.** Neither tool is a ruleset
  dependency, so nothing forces your pin; what changed is which versions the
  generated configs are proven against. `@npm` (`tests/npm/pnpm-lock.yaml`)
  resolves vite 8.2.2 and vitest 4.1.11, one resolution each. Vite 6 / vitest 3
  was the previously-tested lane; the two spellings that move with the major are
  listed in
  [COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#vite-and-vitest).
- `@npm_vite` and `vite/pnpm-lock.yaml` are **deleted**. Nothing outside the
  repository's own tests named that hub. `@npm_features`
  (`tests/npm/pnpm-lock-features.yaml`) is unaffected: it is the pnpm
  patch/alias/peer-variant fixture, resolves neither Vite nor vitest, and is
  declared `dev_dependency`, so it never reaches a consumer's resolution.

### Breaking — npm

- One external repository per npm package is the only implementation. The
  `npm_translate_lock` repository rule and `npm.translate_lock(lazy = ...)` are
  **deleted**, and with them the 1,794-line `ts/private/npm_translate_lock.bzl`;
  `npm/private/npm_translate_lock.bzl` is now only the pnpm lockfile reader. The
  label surface does not move: `@npm//:zod`, `@npm//:types_react` and
  `@npm//:vitest_bin` all still resolve, through an alias hub that downloads
  nothing.
- `npm.translate_lock` gains `patches`, a label list. `patchedDependencies` was
  previously ignored outright and the unpatched upstream tarball installed with
  no warning. Files are matched to lockfile entries by pnpm's own
  `pnpm patch-commit` name,
  `<name with / replaced by __>@<version>.patch`, and every pairing is
  **verified at extension time**. Four failures, each naming the label:
  a label that resolves to no readable file; a file whose sha256 disagrees with
  the digest `patchedDependencies` records; a `patchedDependencies` entry with
  no matching label; and a passed file no entry claims. A patch filename
  starting with `@` cannot be exported by `exports_files(glob(["*.patch"]))`, so
  the unreadable-label message says to list those files literally.
- npm alias specifiers (`h3-v2: h3@2.0.1`) get their own labels. They
  previously collapsed onto the real package's label, so the name the importing
  code uses did not exist as a target.
- `npm_bin`'s `optional_dep_info` attribute is removed; only the deleted
  single-repository path produced it. Platform-matched `optionalDependencies`
  (native sidecars such as `oxlint` → `@oxlint/linux-x64-gnu`) are named
  through `optional_dep_packages`.
- pnpm `catalogs`, `overrides` (including the `parent>child` form) and
  `packageExtensions` need no support code and got none: pnpm resolves all
  three at every use site, so the lockfile already carries concrete versions
  and injected peers. Tests pin that.
- **New hard error: a `packages:` entry with no usable integrity.** Such an
  entry was previously downloaded with no verification and no warning. The check
  runs when the extension is evaluated, over the whole lockfile, so an entry
  nothing depends on is checked too, and it names every offending package with
  the `resolution:` keys it carries. `sha512-`, `sha384-` and `sha256-` are
  accepted; a pre-SRI `sha1-` is not. There is no opt-out. The rejected shapes
  are dependencies with no published tarball: a git dependency, a `file:`
  dependency on a local directory, and a remote tarball pnpm could not hash.
  The first two already failed; the third is a real capability removal. Depend
  on such a package as a workspace member (`link:`) or vendor its files.
  `npm_import`'s `integrity` attribute is now mandatory.
- **A `workspace:*` member is importable by its package name with no
  `module_name`.** The hub target for a pnpm `link:` dependency is a generated
  rule that declares the name, not an `alias`: Bazel resolves an alias before
  any rule implementation runs, so every member used to restate the npm name as
  `module_name`. Members that do set it keep working. The checked-in IDE
  tsconfig still reads the attribute, so set it there too if the editor has to
  resolve the bare import.

### Breaking — `ts_add_package`

- `ts_add_package` now requires `pnpm_lock`, the label of the lockfile of the
  hub the target edits — the same label that hub's `npm.translate_lock()`
  reads. pnpm is pointed at that label's directory with `--dir`.
  `bazel run //:add_package -- <pkg>` previously resolved against the workspace
  root, writing a stray `package.json` and `pnpm-lock.yaml` there. A target
  declared without the attribute fails
  at load time, naming itself and the line to add. The lockfile is also an input
  of the target, so a missing or invisible hub is a build error. Gazelle writes
  `pnpm_lock = "//:pnpm-lock.yaml"` on the target it generates at the workspace
  root; a repository with several hubs wants one `ts_add_package` per hub, named
  after it.
- `--lockfile-dir` is now refused. Only `--dir` was constrained, by appending
  the hub's own, so `--lockfile-dir` still reached the stray root
  `pnpm-lock.yaml`.

### Breaking — `node_modules` trees

- A tree now places **every** resolved version of a package. The old tree keyed
  every destination by package name alone, so when one npm name resolved to two
  versions in a closure both wrote to `node_modules/<name>`, the last copy won,
  and every dependent silently got that version. Now each name's primary version
  keeps the flat top-level
  directory (primary = what the tree's own `deps` declare, else the highest
  version, the rule `@npm//:<name>` already follows), every other version gets
  its bytes once under `.pnpm/<name>@<version>/node_modules/<name>`, and each
  dependent that resolved to one of those gets a relative symlink at
  `<dependent>/node_modules/<name>`. Nothing that resolved before moves.
- **New hard error:** declaring two versions of one name directly on a single
  `node_modules` target. `node_modules/<name>` is one directory. The failure
  names the target, both versions, and the two ways out: depend on one and let
  the other arrive transitively, or split into two targets.
- `NpmPackageInfo` gains `direct_deps`, the per-dependent resolution the links
  are built from. `ts_test`'s auto-generated tree gets the same layout from the
  same builder.
- **A tree is keyed by resolution, not by `name@version`.** pnpm resolves a
  package once per distinct set of peers, so
  `ansi-styles@6.2.3(ansi-regex@5.0.1)` and
  `ansi-styles@6.2.3(ansi-regex@6.2.2)` used to collapse onto one directory.
  `NpmPackageInfo` gains `peer_id`, carrying pnpm's peer suffix, and the store
  path becomes `.pnpm/<name>@<version>_<peer set>/node_modules/<name>`. The
  peer component is a readable prefix plus a digest of the whole set, since a
  nested peer set can run to hundreds of characters. A tree with no
  peer-differing edge is
  unchanged byte for byte; a tree with one gains the files of the resolution
  that was previously dropped.
- **New hard error:** declaring two peer resolutions of one version on a single
  `node_modules` target, the version error one level narrower. The message
  distinguishes it from the version case.
- `ts_test` no longer keeps its own npm-package collector. The duplicate keyed
  by `name@version`, dropping a peer variant before the layout planner saw it.
  Both paths now call one `collect_npm_packages`.

### Breaking — toolchains and repositories

- The `ts.oxc_toolchain()`, `ts.tsgo_toolchain()` and `ts.node()` extension
  tags are removed. `ts.tsgo(version = ...)` is the only remaining tag.
- `rules_typescript_dependencies()` is gone, and with it `ts/repositories.bzl`.
  The `ts` extension declares its own repositories.
- The `@oxc` repository is gone; oxc resolves directly to
  `//oxc_cli:oxc-bazel`, built from source by `rules_rust` for the exec
  platform. `@tsgo` is replaced by per-platform `@tsgo_<platform>`
  repositories.
- `PLATFORM_CONSTRAINTS` is replaced by `//platforms:platforms.bzl%PLATFORMS`.
  The new `//platforms` package is the single platform vocabulary and also
  declares `//platforms:<key>` (for `--platforms`) and `//platforms:is_<key>`
  (for `select()`).
- oxc and tsgo are `exec_compatible_with`-constrained, no longer
  `target_compatible_with`-constrained, so they follow the execution platform.
  A build setting `--platforms` unusually previously lost its compilers.
- New toolchain type `//ts/toolchain:js_tool_type` separates
  node-as-a-build-tool (exec config) from node-as-a-runtime
  (`js_runtime_type`, whose `runtime_binary` is now `cfg = "target"`).
  Registering `@rules_typescript//ts/toolchain:all` picks up both.
- The `node_modules` tree action resolves node through `js_tool_type` like every
  other build action; it was the one left on `js_runtime_type`. It is identical
  where exec and target agree, and builds for the exec platform under
  cross-compilation. `node_modules` and the `ts_test`-internal
  tree rule declare `js_tool_type` accordingly, so a setup registering only a JS
  runtime toolchain fails at analysis.

### Breaking — the IDE tsconfig

- A target's `module_name` gets its own `compilerOptions.paths` entry, so a
  first-party package imported by bare specifier resolves in the editor. The
  `module_name` keys are emitted last, so a first-party name beats a same-named
  npm package.
- `ts_refresh_tsconfig` gains `extra_exclude`, globs appended to the generated
  `exclude`. TypeScript trees that are not in this module's build graph — a
  nested Bazel module, an example workspace in `.bazelignore` — are otherwise
  walked by `tsc` and checked under the wrong `compilerOptions`, since
  `include` is `**/*`.
- The `ts_compile` targets `ts_test` generates from `srcs`, `setup_files` and
  `global_setup` follow the test's `visibility`, defaulting to
  `//visibility:public`. The old hardcoded `//visibility:private` was
  unnameable, so `ts_refresh_tsconfig(deps = ...)` could not reach the npm
  packages only a test declares. To lock them down, set `visibility` on the
  `ts_test`.

### Breaking — bundling, assets and packaging

- `ts_bundle` requires `bundler`. The bundler-less mode concatenated every
  transitive `.js` file behind a `// Placeholder bundle` header, an artifact no
  runtime accepts. Point the attr at a `vite_bundler`, or any target providing
  `BundlerInfo`. `ts_binary` is unchanged: without a bundler it runs the entry
  point `.js` directly.
- `css_module` and `json_library` need the `js_tool` toolchain, and
  `ts_npm_publish` and `next_build` now do too;
  `register_toolchains("@rules_typescript//ts/toolchain:all")` covers all of
  them. `css_module` and `ts_npm_publish` used to shell out to the host `awk`.
- `css_module` **compiles** the stylesheet with postcss-modules and no longer
  extracts class names from it. Each src gains a second output,
  `<source>.exports.json`, holding the export map postcss-modules produced, and
  the `.d.ts` is generated from that map's keys. Consequences:
  - The declared key set grows. `@keyframes` names, `#id` selectors and
    `@value` names are exports and are now declared, so `styles["panel-fade"]`
    type-checks. Code that walked `keyof typeof styles` exhaustively sees new
    members.
  - `composes: x from "./other.module.css"` works, which means it also fails on
    bad input: postcss-modules errors on a name it cannot find, and the other
    file has to be in `deps`.
  - Constructs postcss-modules rejects now fail the build. `:local(...)` inside
    a `:global(...)` group is the one this repo's own fixture used.
  - The values are scoped names Bazel decided:
    `_<name>_<sha256 of the stylesheet, 8 hex>`, from
    `ts/private/css/scoped_name.ts`, with no path in the hash. **Class names in
    a bundle change shape**: `_panel_t1r4u_1` becomes `_panel_<8 hex>`, so a
    snapshot or a stylesheet override written against the old form moves.
  - The knobs that change the answer are attributes of the rule
    (`locals_convention`, `scope_behaviour`, `hash_prefix`, `export_globals`),
    not of a `vite_config`, because the rule is what wrote the `.d.ts`. Setting
    one of them, or `generateScopedName`, or `css.modules = false`, on the
    bundler side — where it was silently authoritative — is now a **hard build
    failure** naming the attribute to use.
  - The guard tests a mark on the function, not its identity: a framework plugin
    driving its own `createBuilder`, as TanStack Start does, makes Vite run the
    plugin factory once per environment, so the resolved config's function is a
    different closure from the checking instance's.
  - `ts_bundle`, `ts_dev_server` and `ts_test` install a Bazel-owned Vite plugin
    that hands Vite the map, so the bundler's CSS-modules pass reproduces the
    names.
  - A new non-dev npm hub, `@npm_css` (`ts/private/css/pnpm-lock.yaml`, 19
    pure-JS packages), lands in every consumer's `MODULE.bazel.lock`. It is the
    compiler, so it cannot be dev-only: `css_module` is consumer API.
    `bazel run //:add_package_css -- <pkg>` edits it.
- A name that is not a bare TypeScript identifier is emitted quoted
  (`readonly "button-primary": string`) and is no longer a syntax error.
- `ts_npm_publish` stages into `<name>_pkg/package/`, not `<name>_pkg/`, and
  its `package.json` is rewritten through `JSON.parse`/`JSON.stringify`, so a
  template that is not one-field-per-line survives. Output is pretty-printed
  two-space JSON.
- `ts_npm_package` requires `package_dir` and `package_files`; the impl always
  dereferenced both.
- **`ts_bundle`'s generated Vite config no longer names anything Vite removed
  or made optional.** `split_chunks` emits
  `build.rollupOptions.output.manualChunks` in place of
  `splitVendorChunkPlugin`, which Vite 7 removed. `minify = True` emits `true`,
  the running Vite's own default minifier (esbuild on 6, oxc on 8), and no
  longer names `"esbuild"`: Vite 8 accepts the name but then installs
  `buildEsbuildPlugin()`, and esbuild is an optional peer, absent from a tree
  built from `deps = ["@npm//:vite"]`. `minify = False` now also emits
  `output.minify: false`, because `build.minify: false` alone still runs the
  dead-code pass, which re-emits each chunk from its AST and silently discards
  whatever a plugin's `renderChunk` returned, string form and `{code}` form
  alike.

### Breaking — tests

- `tests/bootstrap` is deleted, all 8 targets. Every scenario it covered exists
  under `tests/integration/`, with ten more besides; the bootstrap copies
  inherited `PATH`/`HOME`/`USER` and found `bazel` on the caller's PATH. Use
  `bazel test //tests/integration/...`. The `RULES_TYPESCRIPT_ROOT` environment
  variable those runners needed is no longer read anywhere.
- Integration targets carry neither `manual` nor `exclusive`, so
  `bazel test //...` runs them and runs them concurrently. They carry
  `nested-bazel`, `cpu:2`, `no-sandbox` and `external`;
  `bazel test --config=fast //...` filters `-nested-bazel` and skips them.
  Count what your tree has with
  `bazel query 'attr(tags, "nested-bazel", tests(//...))' | wc -l`.
- The nested-Bazel workspaces share one repository cache. Each has its own
  output base, so each used to fetch the whole BCR registry separately and the
  concurrent lookups failed on a different subset each run. `prepare()` in
  `tests/integration/harness/harness.go` appends
  `common --repository_cache=<shared>` to each staged workspace's `.bazelrc`.

### Breaking — the dev server

- `bazel run //app:dev` serves **first-party source**. Vite transforms the
  checked-in `.ts` in memory, with no Bazel analysis-and-action cycle between a
  keystroke and the browser. `bazel-bin` stays authoritative only
  for what Vite cannot produce itself: the npm tree, `ts_codegen` output,
  assets and passthrough `.d.ts`. Generated code is recognised by having no
  checked-in source, not by a path list. It previously served the compiled `.js`
  from `bazel-bin`, which put a Bazel rebuild in the inner loop.
- **The dev server no longer type-checks.** That is native Vite parity: a type
  error surfaces in the editor and in `bazel build`, and no longer blocks the
  browser update. `ts_bundle` is unchanged: a production bundle still consumes
  Bazel's compiled output.
- The restart decision moved into the Vite process. `ibazel run` SIGTERMs the
  launcher after every rebuild and the launcher deliberately survives it, so
  one Vite process lives across rebuilds. A `ConfigWatcher` compares content
  digests of the inputs the generated config was built from (the config itself,
  the npm tree, the toolchain node binary) and restarts only when one changes.
  Neither a `ts_codegen` rebuild nor a source edit restarts the server.
- **A bare npm specifier from dev-served source resolves.** The generated
  config set `resolve.modules`, a webpack option Vite ignores, so a served
  module importing `"react"` answered 500. A `bazel:npm-resolve` plugin
  (`enforce: 'pre'`) finds the package's own `package.json` inside the
  `node_modules` tree and hands the id back to Vite's own resolver anchored
  there, so exports maps, conditions and subpaths stay Vite's to interpret. A
  package the tree does not carry falls through to Vite's ordinary
  unresolved-import error.
- **`react_refresh = True` fails loudly and no longer serves without Fast
  Refresh.** The rule imported `@vitejs/plugin-react/dist/index.mjs` by fixed
  path, a file the installed major does not ship, and swallowed the failure in a
  `console.warn`. The entry point now comes from that package's own `exports`
  map, and a load failure throws naming the `ts_dev_server` label and the dep to
  add. A dev server whose `node_modules` target is missing
  `@npm//:vitejs_plugin-react` now refuses to start.
- **`vite_config` is loaded from a copy in `bazel-bin`, not from your source
  tree.** Node resolves a runfiles symlink before that file's own imports, and
  this ruleset has no source-tree `node_modules`. The consequence is a boundary:
  a bare npm specifier in the config resolves through the tree
  the `node_modules` attr built, provided that target is in the same Bazel
  package as the dev server; a relative import does not, because only the one
  file is copied, and the server exits with
  `[rules_typescript] Failed to load vite_config` naming the file. `ts_bundle`'s
  `vite_config` is unchanged and still imported from the source tree.

### Breaking — shell replaced by Go

- `scripts/` is deleted. `ci.sh` duplicated `.github/workflows/ci.yml`;
  `quickstart.sh` and `release.sh` are `bazel run //tools/quickstart` and
  `//tools/release`; `verify_determinism.sh` is explicit CI steps, since two
  builds cannot be one action.
- `ts_binary`, `ts_test`, `ts_dev_server` and `npm_bin` run through one
  checked-in Go launcher that reads a per-target JSON config. The generated file
  is `<target>_launcher` — `<target>_test_launcher` from `ts_test`,
  `<target>_bin_launcher` from `npm_bin` — **not** `<target>_runner.sh`;
  anything naming the old file breaks. `--dump-config` prints the resolved
  config.
- `tools/refresh_tsconfig.sh` is deleted. `ts_refresh_tsconfig` takes `deps`
  and an aspect walks them; a `deps = []` call produces a tsconfig with no
  `paths` at all. Its `execroot_symlink` attr is removed.
- `npm_import`'s `expected_prefix` attr is removed; the prefix is detected.

### Added

- **`ts_binary` takes a plain JavaScript file as its `entry_point`.** The attr
  is polymorphic: a target providing `JsInfo` behaves exactly as before, and a
  single `.js`, `.mjs` or `.cjs` source now runs as-is. A Node generator for
  `ts_codegen` no longer needs an `sh_binary` wrapper to become an executable:

  ```python
  ts_binary(
      name = "paraglide_compile",
      entry_point = "scripts/paraglide-compile.mjs",
      data = ["scripts/paraglide-compile-dts.mjs"],
      node_modules = "//web:node_modules",
  )

  ts_codegen(
      name = "compiled",
      srcs = ["project.inlang/settings.json"] + glob(["messages/**"]),
      out_dir = "compiled",
      generator = ":paraglide_compile",
      node_modules = "//web:node_modules",
  )
  ```

  A new `data` attr carries the modules the entry imports into runfiles; it is
  extra runfiles for either `entry_point` shape. A `.ts` entry point is refused
  with a message naming `ts_compile` rather than compiled implicitly, which
  would be a second compile surface with no `deps`, no `tsconfig` and no choice
  of declaration emitter. `entry_point` no longer declares
  `providers = [JsInfo]`, so a target providing neither now fails in the rule
  with the message the rule already carried instead of in attribute checking.

  `//tools/codegen:tanstack_routes`, the route-tree generator this ruleset
  ships, is the first user: its 102-line shell wrapper — which parsed `--out`
  and `--srcs` by hand and wrote a `.mjs` file out of a heredoc at build time —
  is now a 93-line `.mjs` and a three-line `ts_binary`.
- **A `ts_codegen` directory can be a `ts_compile` dep.** `out_dir` is the only
  output shape a generator whose file names come from its input can have, and
  until now nothing downstream could read one: the rule returned `DefaultInfo`
  alone, so naming it in `deps` failed analysis with `does not have mandatory
  providers`. An `out_dir` target now carries `JsInfo`, `TsDeclarationInfo` and
  `TsModuleInfo` — the same fields a `ts_compile`'s own outputs travel in, not a
  parallel set for directories — and the new `module_name` attribute names the
  specifier importers write:

  ```python
  ts_codegen(
      name = "messages",
      out_dir = "compiled",
      module_name = "#app/messages",
      ...
  )

  ts_compile(name = "app", srcs = ["main.ts"], deps = [":messages"])
  ```

  The tree goes in `deps`, never `srcs`: `srcs` declares one output per input
  file at analysis time and a directory has no file list to declare from. So the
  generator has to emit compiled output — `.js` beside `.d.ts` — because nothing
  downstream will compile it. `module_name` without `out_dir` is an
  analysis-time error.

  The undeclared-import check reads a directory as a path prefix rather than as
  a file list, and reports the label when a relative import lands inside a tree
  that arrived only through another dep.
- **`ts_worker_deploy` uploads a Cloudflare Worker.** The ruleset could only ever
  dry-run one, so authentication, whether Cloudflare accepts the bundle, routes
  and cron triggers had never been exercised. It is a `bazel run` target, the
  shape Bazel has for a side effect and the one `rules_oci` gives `oci_push`:
  `bazel build //...` writes the launcher, `bazel test //...` cannot select a
  non-test rule, and only `bazel run` uploads. Same attributes as
  `ts_worker_dry_run`, `env_name` included, and the command line it builds is
  the dry run's minus `--dry-run`.

  The launcher's default is still the dry run. A launcher config that does not
  say `"command": "deploy"` — older, hand-written, or misspelt — dry-runs, and an
  unrecognised value fails the config outright. In the other direction, a dry-run
  target now refuses `--no-dry-run` and `--dry-run=false`, which yargs' boolean
  negation would otherwise have honoured, and it has the `CLOUDFLARE_*` and
  legacy `CF_*` variables removed from the environment wrangler is given — so
  "no credentials and no network" is now enforced rather than merely intended.
  `ts_worker_deploy` is the only one of the three that keeps the ambient `HOME`,
  `CI` and credentials.
- **Gazelle names a package's own `tsconfig.json` on the targets it
  generates.** Nothing it wrote ever did, so every generated target compiled
  against the ruleset's baseline alone while the repo's own `lib`, `types`, `jsx`
  and strictness sat unread beside it — a worker whose tsconfig declares
  `"lib": ["es2022"]` failing on the globals that `lib` grants. Every generated
  `ts_compile` and `ts_test` now names the nearest hand-written `tsconfig.json`
  walking up, the way tsserver resolves one, and a `ts_config` target beside
  that file makes it a label a subpackage can reach. `deps` on that target — the
  `extends` chain Starlark cannot read — is yours and survives every run without
  a `# keep`.

  The `ts_config` is written even into a directory that holds nothing else, which
  is what the pnpm workspace-member layout is: `package.json` and `tsconfig.json`
  beside each other with the sources under `src/`. Without it the label names a
  target in a package Bazel never loads, and that fails analysis for the whole
  workspace.

  Three cases get no attribute rather than a label into a directory Gazelle
  writes no BUILD file into, each logged with the fix: a directory under a
  `# gazelle:ts_ignore` or inside a tree Next.js or SvelteKit stages by glob;
  under `index-only` or `tsconfig` boundaries, one that is not already a package,
  where a BUILD file written just to hold the `ts_config` would stop the roll-up
  walk and drop every source beneath it; and one whose own target is already
  named `tsconfig`. The `tsconfig.json` files `ts_refresh_tsconfig` writes are
  skipped — they are built out of the very targets that would name them.

  Naming a tsconfig adds its options and removes none: see the `ts_compile`
  breaking note above for the baseline that now applies in both modes, which is
  what keeps a plain `gazelle` run over a working build from changing what any
  existing target already compiled with.
- **`# gazelle:ts_ambient_types` declares ambient `@types` for a whole tree.**
  Every dep Gazelle writes comes from a specifier in a source file, so an
  ambient declaration — `process`, `Buffer`, `__dirname`, a test global — has
  nothing to infer from, and was the one strict-deps failure
  `bazel run //:gazelle` could not repair. The directive appends its labels to
  every generated `ts_compile` and `ts_test` in the tree, a target that imports
  nothing included, and subdirectories inherit it and may add their own. It
  changes nothing about what the compiler accepts: the dep must exist,
  and `ts_compile` still names only direct `@types/*` deps in the tsconfig.
- **SvelteKit, Remix SSR and Svelte components have rules.** `sveltekit_build`
  and `remix_build` each wrap the framework's own Vite build as one action and
  return both halves: the browser bundle under `client/`, the request handler
  under `server/`, staged from declared inputs with the network blocked.
  `svelte_library` compiles `.svelte` files, which `ts_compile` cannot read,
  into files the rest of the graph can carry. SvelteKit previously got no
  buildable target at all; Gazelle now generates `sveltekit_build`. Integration
  tests:
  `sveltekit_test`, `remix_ssr_test`, `svelte_test`, `tanstack_test`.
- **`next_dev_server` and `next_serve`.** `next dev` over your source tree for
  the inner loop, and `next start` over a staged copy of the build to check that
  the built app server-renders. Gazelle writes the dev target beside
  `next_build`.
- **A `*.module.css` or an imported asset can be bundled.** `ts_bundle`
  collected only `CssInfo` from its entry point, so a `.module.css` or an `.svg`
  was never in the sandbox and Vite resolved the relative import onto an empty
  `bazel-bin` path; `css_module` and `asset_library` also
  did not copy their sources into `bazel-bin` the way `css_library` does. All
  three providers now reach the bundle action, and both rules copy.
  `//tests/vite_bundle:bundle_assets_test` builds an app-mode bundle whose entry
  imports both and asserts the asset lands under a content hash.
- **`ts_bundle` gained `public_dir` and `manifest`, both app mode only.**
  `public_dir` stages a filegroup into a directory of its own and hands it to
  Vite as `publicDir`, so those files are copied into the bundle verbatim and
  unhashed. `manifest = True` writes `manifest.json`, mapping each input to the
  hashed file it became. Both fail at analysis time in lib mode, which declares
  its output filenames and does not hash them.
- **The keys and values a `*.module.css` declaration promises are checked
  against the ones the bundler emitted.** A bundle fixture dumps the real export
  map through `css.modules.getJSON` and compares it to the `.d.ts` and
  `<source>.exports.json`, then looks for every value in the emitted stylesheet.
  The `@keyframes` divergence this entry used to record is gone.
- **App-mode asset hashing is pinned.** `//tests/vite_bundle:app_mode_test`
  checked only that some `.js` existed; it now requires the hashed chunk name
  and the emitted HTML's reference to it.
- **`bazel coverage` works on a test whose pool runs in a second runtime.**
  `ts_test` gained `coverage_provider` (`"v8"`, vitest's own default, or
  `"istanbul"`). v8 coverage is read out of Node's inspector, which workerd has
  no equivalent of, so a `@cloudflare/vitest-pool-workers` test needs the
  transform-time provider; the launcher no longer forces `v8` on the command
  line, where it outranked every config layer. Two pieces moved with it: the
  Bazel config layer sets `test.coverage.allowExternal`, without which vitest
  instruments nothing under Bazel, and the lcov rewrite resolves a source path
  that escapes the run directory back to its package path.
  `//tests/workers:worker_test` reports `LH:4 LF:4` for
  `tests/workers/src/index.js`, from code that ran inside workerd.
- `ts_compile` can consume a real `tsconfig.json`: the generated config
  `extends` it in place and does not copy it, so relative paths inside it still
  resolve. This unblocks ambient-globals-only packages
  (`types: ["./worker-configuration.d.ts"]`), `jsx: preserve` with
  `jsxImportSource`, `lib: ["webworker"]`, `resolveJsonModule` and
  `allowImportingTsExtensions`.
- npm packages are fetched on demand. A target's npm cost is its own dependency
  closure, not the whole lockfile; repositories fetch in parallel, caching and
  invalidation are per package, and a malformed tarball fails only its own
  package.
- `//tests/lsp:test_tsserver_diagnostics` passes for the first time. It wanted a
  `typescript` module from a host `node_modules` that does not exist on a clean
  machine; `typescript` now comes from the lockfile through `@npm`.
- `//tests/integration/npmrc_registry` covers `.npmrc` private registries
  against a throwaway local registry that 401s unauthenticated requests: a
  scoped `registry=` override, and an `_authToken` reaching the wire as
  `Authorization: Bearer`.
- A root `go.mod` and `go.work`, so `go vet`, `staticcheck` and `gopls` reach
  the ruleset's Go. `go vet` immediately found a redeclaration Bazel cannot see:
  three files in one directory each declared `const placeholder`, one single-src
  `go_test` apiece. CI gates on `gofmt -l` and `go vet`.
- All six `examples/` workspaces are in the CI matrix. It built two, so
  `examples/tanstack-app` was broken with nobody watching.
- **`//vite/tests:peer_version_test`** reads `peerDependencies.vite` out of
  `vite/package.json` and asserts the Vite the tests install is a major that
  range names. That coupling was briefly a second npm hub (`@npm_vite`, on its
  own lockfile). See
  [COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#vite-and-vitest).
- `//tests/integration:remix_test`, a nested-Bazel journey through a real Remix
  workspace: Gazelle, `bazel build` on what it wrote, then assertions on the
  artifacts (`client/index.html` with hashed refs, `client/.vite/manifest.json`,
  one hashed chunk per route). It also asserts the failure each load-bearing
  attr prevents: dropping `package.json` from `staging_srcs` fails on
  `_staging/package.json`, and a root-relative `entry_point` fails
  `bazel build //...` on `'//:entry_client' does not exist`.
- `//tests/vitest/snapshot` and `//tests/vitest/environment:{dom,node}_test`,
  the first tests here to run a snapshot assertion and a non-default vitest
  environment. The environment pair asserts the same claim from both sides — one
  needs a `document`, the other needs there to be none — so an `environment`
  that never reached vitest fails one of them. `jsdom` and `edge-runtime` stay
  analysis-only behind a `build_test`.

### Fixed

- **A `tsconfig` `paths` value under the tsconfig's own directory no longer
  fails with `TS5090`.** The value is computed relative to the generated
  tsconfig, and a target below that directory relativised to a bare segment —
  `compiled/index.d.ts` — which TypeScript reads as a package name now that tsgo
  has removed `baseUrl`. Every `paths` value is written explicitly relative.
  Reached by any `module_name` or `path_aliases` target whose root sits under
  the consuming package's bin directory; a `ts_codegen` tree always does.
- **A directory in `ts_compile` `srcs` says what to do about it.** It used to
  report `(extension: .)` from the file-type check. It now names the attribute
  the tree belongs in.
- **The undeclared-import check sees a `#` specifier at all.** Every specifier
  had its URL fragment stripped from the first `#`, and Node calls a
  `#`-prefixed name a package-private import -- the whole specifier, not a
  fragment on an empty one. So `#shared/messages` became `""` and every one of
  them was silently exempt from the check. A `#` after the first character is
  still a fragment.
- **A dep's tree artifact no longer kills the undeclared-import check.** Every
  provider route into `_strict_deps_check` fed the manifest through `add_all`,
  which expands a directory the action holds no input for:
  `Failed to expand directory <generated file .../compiled>`. Measured on all
  four: `TsDeclarationInfo.declaration_files`, `.transitive_declaration_files`,
  `JsInfo`, and `path_alias_srcs`. Expansion is off, and the directory enters
  the manifest as the one path Bazel knows.
- **A `glob()` in a `# gazelle:ts_codegen` `srcs:` field reaches the BUILD file
  as Starlark.** Both ways of writing one were broken. A `srcs:` field holding
  only a glob was written as a quoted string -- `srcs = "glob([\"messages/*.json\"])"`
  -- which is a string on a `label_list` attribute, so Bazel refused to load the
  package at all: *expected value of type 'list(label)' for attribute 'srcs'*. A
  glob beside a file name was silently dropped instead, leaving a target whose
  generator read fewer inputs than the directive named and no diagnostic saying
  so. The field is now split on the commas between entries rather than every
  comma, so a glob keeps its own patterns and its `exclude`, and the srcs
  attribute is built as a real expression: `["settings.json"] +
  glob(["messages/*.json"])`. An entry that opens `glob(` and does not parse as
  a `glob()` call is refused with a log line and no rule, rather than being
  dropped. The `dir:` out_dir form is documented for what it is: Bazel declares
  the directory as one artifact, so nothing in it has a label, no
  `<name>_compile` is written, and reaching the output means a rule of your own
  that adapts the directory to the providers `ts_compile.deps` reads.
- **A directory a `ts_codegen` glob collects is no longer made into a package of
  its own.** Rendering the glob correctly was not enough to load it: a `srcs:`
  glob reaching into a subdirectory of catalogues met a `json_library` per file
  there, which makes that directory its own Bazel package -- and `glob()` does
  not descend into one, so the pattern matched nothing and Bazel rejected the
  package above it outright (*glob pattern didn't match anything, but
  allow_empty is set to False*). Those files are the ancestor rule's inputs, so
  Gazelle now writes no targets over them and the directory stays part of the
  package that globs it. Where that is not enough -- a file the glob does not
  collect still needs a target, and that target restores the package -- Gazelle
  names the leftovers in a log line instead. `gazelle_roundtrip` now runs
  Bazel over a generated tree carrying such a directive: the converge tests
  compare BUILD text, and neither this nor the quoted-glob bug was visible in
  text.
- **A `module_name` dep in a package below its consumer builds again.** The
  generated tsconfig names each module root relative to its own directory, and a
  producer nested under the consumer is the one shape whose relative path has no
  leading `..` to give it away: `//web:web` depending on `//web/shared/i18n:x`
  wrote `shared/i18n/compiled`. TypeScript reads a `paths` value with no visible
  `.` as a module specifier rather than a path and rejects the config outright --
  `error TS5090: Non-relative paths are not allowed` -- so the target failed
  before tsgo read one import, and no `module_name` dep nested under its consumer
  could be declared at all. Every `paths` value the rule writes now carries an
  explicit `./` when it has none of its own: `path_aliases`, npm entry points,
  npm subpath declarations and both roots of a `module_name`. The editor tsconfig
  `ts_refresh_tsconfig` writes was already right -- its values are
  workspace-root-relative and every one is written with the prefix -- and a test
  now pins that.
- **A dep is no longer fabricated for a directory that does not exist.** When
  no indexed rule provides an unresolved specifier, Gazelle names the package
  that would have to: `//src/generated/api` for `@/generated/api`. It never
  checked that the directory was there. A specifier that maps inside the
  workspace but points at nothing -- an `imports` entry for a codegen output
  that has not been generated, most easily -- therefore produced a label Bazel
  answers with `no such package`, which fails **analysis for every target in
  the build**, where the missing module alone would have been one `TS2307` from
  the compile. The two neighbouring cases (a file path read as a directory, an
  unclassified extension) were already guarded for exactly this reason; the
  missing directory was not. The fabricated label now requires a directory on
  disk. A directory that exists but is not a Bazel package is unchanged.
- **A `#` specifier resolves through the package's `imports` field.** Node calls
  a `#`-prefixed specifier a package-private import, and the `imports` map in
  the importing package's own `package.json` is the only thing that answers one.
  Gazelle never read that field, so `#shared/flags` fell through to the bare-
  specifier branch and came back as `@npm//:#shared` -- a label whose target no
  hub declares, which fails analysis for the whole build rather than dropping
  one dep. The map is now read as a path alias (conditions objects and
  alternative arrays included), below `compilerOptions.paths`, which is what
  kept the one monorepo measured working: it duplicates both entries into
  `paths`. Nearest wins: an inner package's map replaces an outer package's
  answer for the same key, the way Node answers a `#` from the nearest
  enclosing `package.json` rather than the outermost. An entry whose target
  names another package rather than a path -- `{"#dep": "lodash"}`, the
  conditional-polyfill shape the field exists for -- resolves to that package's
  label. A wildcard that is not a trailing `/*` is dropped: an alias key matches
  by prefix, so a key holding a literal `*` could never fire. A `#` specifier no
  entry covers at all resolves to nothing.
  `paths`. A `#` specifier no entry covers now resolves to nothing.
- **A `json_library` parse error names the line the mistake is on.** The rule
  parses the stripped text, so the line and column in
  `json_library: failed to parse ...` come from that text and not from the file
  a person edits. `Strip` deleted a block comment whole, and deleted the
  whitespace between a trailing comma and its closing brace -- newlines
  included, while the `//` branch had always emitted one. A block comment
  spanning four lines above the mistake reported it three lines early (line 7
  read as line 4); a trailing comma whose `}` is on the next line cost one more
  (line 6 read as line 5). Both branches now leave their newlines where they
  were, and each half is load-bearing on its own.
- **A checked-in `*.gen.ts` is a source again.** Gazelle dropped every file
  whose name ended `.gen` / `.generated` / `.auto` from every source target, on
  the guess that something in the build produced it. The guess was already
  unnecessary: `claimedSrcs` keeps out exactly what a `ts_codegen` in the
  package declares as an out. What the name check added on top was the
  checked-in generated file nothing produces -- 204 of them in one monorepo,
  under one directory, behind 416 `TS2307`s from the
  `#shared/lib/flags/gen/<name>.gen` specifiers that import them. Only
  `routeTree.gen.ts` is still recognised by name, because the build really does
  write it: the Start Vite plugin regenerates it into the staging tree, and the
  tree it emits imports the router module that imports the tree back -- a cycle
  between two Bazel packages however the two are split. **The edit:** a
  workspace that wants some other generated-looking file out of `srcs` names it
  in `# gazelle:ts_exclude`.
- **`json_library` reads JSONC, so a commented `.json` gets its `.d.ts`.**
  TypeScript accepts comments and trailing commas wherever it reads JSON, and
  Gazelle already decoded `tsconfig.json` through a stripper for exactly that
  reason -- but the rule's own build-time read was a strict `JSON.parse`, so the
  same file meant two things depending on which side read it. The stripper moved
  to `//gazelle/jsonc`, and `json_library` now strips through
  `//gazelle/jsonc/strip` before parsing: one implementation of the dialect,
  shared by the BUILD generator and the rule. Four `json_library` targets on one
  monorepo built again, among them two `tsconfig.*.json` files -- files Gazelle
  itself reads as JSONC. This is the declaration and the type-check; it is not a
  runtime claim. A `json_library` `.json` reaches no bundler at all today --
  nothing copies it into bazel-bin and it carries no `AssetInfo`, so a
  `ts_bundle` over `import data from "./data.json"` fails to resolve the
  specifier whether or not the file has a comment in it (TODO.md, "Known gaps").
- **`ts_test` roots Vite at the package, so a relative path in a user config
  means what it says.** Vite's root defaulted to the working directory, which
  under Bazel is the runfiles root -- one tree above the package. Every relative
  path a config author writes was resolved from there: a `setupFiles: ['./x.mjs']`
  looked for `<runfiles>/x.mjs`, and `@cloudflare/vitest-pool-workers` resolved
  `wrangler: { configPath: 'wrangler.jsonc' }` to `<runfiles>/wrangler.jsonc` and
  failed the run with `Could not read file`. The generated config now sets
  `root` to the package directory the launcher already exports as
  `TS_TEST_PACKAGE_DIR`. It is in the Bazel layer, so a config that wants a
  different root still wins.
- **A src listed from outside this package's tree fails while the BUILD file
  loads.** A source is compiled into the package of the target that lists it,
  but the root its package-relative path hangs off is where the file actually
  lives, so a src from a sibling package, an ancestor or another repository
  hangs off a root of its own while the listing package's own sources hang off
  the package -- and one tsgo declaration emit has one `rootDir`. That was
  already an error -- at analysis time, printing the exec root as an empty line,
  never naming the file, and blaming "a mix of checked-in and generated
  sources", which is a different cause. The srcs list is a loading-phase fact,
  so the half of the rule it can decide is now decided there, naming every file.
  Only the mix is rejected, and six shapes are not it: `declarations = "oxc"`
  and `enable_check = False`, the two escape hatches the analysis-time error
  already offers; a `.d.ts`, passed through rather than compiled, which is what
  makes `vite_types = True` legal from any package; a *descendant* package's
  src, which is already inside this package's directory and hangs off the same
  root -- a ts_compile may hold a subtree, and a subtree may grow a BUILD file;
  a target whose srcs all come from elsewhere, which has one root like any
  other; the top-level package, which IS the exec root a foreign src hangs off;
  and a `select`, whose branches resolve after loading is over. Only a label
  that names a source file is judged -- `//other:some_target` stands for files
  the loading phase cannot place. `@//pkg:f` and the canonical `@@//pkg:f` are
  this repository, so `@@//<this package>:f` is this package. The analysis-time
  check stays for the half a srcs list cannot show, a generated source in this
  same package.

- **Gazelle reads `pnpm-lock.yaml`, so the npm inventory everything gates on is
  no longer empty.** `tc.npmPackages` -- "which npm packages does this
  workspace declare" -- was populated in one place only, from the deprecated
  `gazelle_ts.json` `npmMappingFile` key. Gazelle detected the lockfile's
  existence to decide whether to emit `//:pnpm` macros and never read its
  contents, so on any repo without a `gazelle_ts.json` the inventory was nil and
  every check against it silently did nothing: a `node:fs` import got no
  `@types/node` dep (228 `TS2591` plus 8 `TS2503` on one monorepo measured), the
  prisma and graphql-codegen detectors emitted a `@npm//:<tool>_bin` on file
  presence alone, and `filterNpmDeps` kept every configured framework dep
  whether the workspace had it or not. The lockfile is now parsed once per run
  into the set of names the `@npm` hub declares a flat label for -- the resolved
  closure, workspace `link:` members and npm aliases included -- for lockfile
  formats 6.x and 9.x. An unsupported version logs and leaves the inventory
  absent rather than empty, which keeps the heuristics for a workspace whose
  lockfile could not be read while turning them into real checks for one whose
  could. `gazelle_ts.json`'s mapping file still wins per package it names.
- **`# gazelle:ts_codegen` writes the targets it names, and imports of the
  generated module resolve to them.** The directive parsed into a pattern that
  carried no `srcs`, and a pattern with no `srcs` produces no rule, so the
  directive did nothing at all. It takes an optional `srcs:<csv>` field now and
  defaults to the directory's own sources without one. Alongside the
  `ts_codegen` Gazelle writes `<name>_compile`, the `ts_compile` that carries
  the codegen label in `srcs` with `declarations = "oxc"` — the generated module
  has to reach a compile through `srcs`, since `ts_compile.deps` requires
  providers `ts_codegen` does not return — and indexes every declared out
  against it, so `import "./schema.gen"` gets a dep with nothing hand-written.
  A declared out that is also checked in is kept out of `ts_compile.srcs`,
  whether the rule declaring it comes from the directive or was already in the
  BUILD file: the file being both a source and an output of its package is a
  conflicting declaration Bazel rejects. Deleting the checked-in copy is
  therefore optional, and works either way.
- **A pnpm workspace member is importable by every specifier its
  `package.json` declares.** The `paths` map hardcoded `<root>/index.d.ts` for a
  member's bare specifier and `<root>/*` for everything under it, so a member
  entered through anything else was `TS2307` at the root, and every `exports`
  subpath was `TS2307` unless the specifier happened to mirror the source
  layout. A design-system package with a subpath per component — 99 `exports`
  keys, `./button` → `./components/controls/button/index.ts` — resolved almost
  none of them. The hub target now reads the member's own manifest: `exports`
  in the map's own key order, then `typings`, `types` and `main`, each
  designated target answered with the declaration Bazel emits from it. Every
  declared subpath gets its own `paths` key and a wildcard subpath gets a
  wildcard pattern, with the old guesses kept behind them — so a manifest
  naming a file the build does not produce resolves as it did before, and a
  member that declares nothing generates a byte-identical tsconfig. `module` is
  not read: no TypeScript resolution mode consults it. A subpath excluded with
  `null`, and one naming something no compiler emits a declaration from, get no
  entry; enforcing that they must not resolve is the strict-deps check's job,
  not this map's.
- **The editor tsconfig reads that manifest too.** `ide_tsconfig` writes the
  workspace-root `tsconfig.json` an editor resolves against, and it carried the
  same two guesses — `<root>/index` for the bare specifier and `<root>/*` for
  everything under it. So an editor answered `@scope/pkg/button` with a file
  that is not there while the build answered it from the manifest, which is a
  divergence between the two programs rather than one wrong answer. It now
  writes the declared entries first, each in the source tree and under
  `bazel-bin`, with the guesses behind them exactly as the compile tsconfig
  keeps them.
- **A member's `package.json` is found where its label says it is.** Member
  paths were resolved against the lockfile's own directory while
  `link_target_label` writes `@@//<path>`, so a lockfile in a subdirectory
  looked for members beside itself and named them somewhere else. The two
  coincide for a lockfile at the workspace root, which is every real one.
- **`typings` is read before `types`**, the order
  `readPackageJsonTypesFields` reads them in. Both layers that resolve a
  declaration entry point now agree.
- **An npm alias resolved against peers gets its hub label.** An alias declared
  by an importer was recorded with its peer suffix stripped —
  `tailwindcss@3.4.18` for a dependency pnpm resolved as
  `tailwindcss@3.4.18(tsx@4.23.12)(yaml@2.8.1)` — and that value is a
  `packages:` key, while the hub looks an alias up among the `snapshots:`. It
  matched nothing, so `@npm//:<alias>` did not exist at all and the
  per-importer label pointed at a target the package's own repository was never
  told to declare. An alias whose package pnpm resolved without peers was
  unaffected, which is why the flat-hub alias support looked complete. A
  `catalog:` entry is the common way to reach the broken half: the catalog names
  the version, the use site says `catalog:`, and the resolution carries whatever
  peers the package has.
- **A pnpm `link:` workspace member reaches the runtime `node_modules` tree.**
  The hub target for a `workspace:*` dependency forwarded the providers a
  compiler reads and none that a `node_modules` tree is built from, so a
  `ts_test` depending on `@npm//:<member>` type-checked and then died in Node's
  resolver with `Cannot find package '<member>'`. The hub target now describes
  the member as an npm package -- its files, its own npm closure, and a
  generated `package.json` marking it ESM -- and it stages like any other.
- **The hub looks up a `link:` member's target instead of deriving it from the
  member's entry point.** The label was `//<member>/<dir of main>:<basename>`,
  which is one of several targets Gazelle might have generated and is right only
  by coincidence: `# gazelle:ts_package_boundary tsconfig` rolls a member's
  subtree up into the directory holding `tsconfig.json`, so a member with
  `main: src/index.ts` builds from `//packages/x:x` and not from
  `//packages/x/src:src`, and `# gazelle:ts_target_name` renames the target
  outright. The hub now walks from the directories the member's manifest
  designates an entry point in -- `main`, `module` and `exports["."]`, so a
  member that declares only an exports map is walked too -- up to the member's
  root, and takes the innermost one declaring a target of that name, honouring
  `ts_target_name`; a directory becoming a package refetches the hub. When no
  candidate declares it the hub declares no target for that name and writes a
  comment saying why, and that covers a member whose `BUILD.bazel` exists and
  declares something else as much as one with no `BUILD.bazel` at all -- an
  undeclared target fails only what asks for the member, while a label naming a
  target Bazel cannot resolve fails analysis for everything that reaches the hub.
- **Gazelle converges.** The framework generators were create-if-absent
  (`if !ruleExists(...)`), so the second run emitted no candidate and the rule
  froze at whatever the first run wrote: a file added to a staged directory was
  absent from the build with nothing failing. They regenerate on every run;
  `TestConvergeAfterMutation` covers 58 cases across six workspace shapes.
- **A value Gazelle drops is named, and a deleted one is not.** The set of
  attributes Gazelle owns and recomputes is derived from `Kinds()`, which had
  been hand-listed and left `ts_compile.deps` unguarded. Every dropped value is
  reported with the `# keep` that would hold it. Two cases stay silent: a value
  whose file or package is gone from disk, where `# keep` would name a source
  nothing provides; and anything already carrying `# keep`. An expression the
  merger cannot reconcile value by value (a variable, `a + b`, a `select()`) is
  reported as no longer maintained.
- **Gazelle's Node-builtin list is checked against the Node it runs.** The
  hand-maintained list was missing 15 bare names: `sys` and the `_http_*`,
  `_stream_*` and `_tls_*` legacy aliases, so `import "sys"` got a dep on
  `@npm//:sys`, a label no hub declares. In the same helper,
  `# gazelle:ts_warn_unresolved` logged "unresolved import" for every builtin
  sub-path (`fs/promises`, `timers/promises`, `stream/web`, `util/types`).
  `tests/strict_deps/builtins_test.go` reads `builtinModules` out of the
  toolchain Node the check action runs and asserts set equality.
- **A consumer whose lockfile has no esbuild can build the dev-server plugin.**
  `//vite:esbuild_node_modules` named `@npm//:esbuild`, and inside a consumer's
  build `@npm` is their own lockfile. That tree feeds
  `//vite:vite_plugin_bazel`, which `ts_dev_server` takes through `plugin` and
  Gazelle writes with a dev server. esbuild now comes from `@npm_css`, at the
  version that hub already pinned. `examples/react-app` carries the `plugin`
  attr Gazelle would write, and `//tests/npm:vite_plugin_hub_test` and
  `:css_compiler_hub_test` assert by aquery that neither tree reads `@npm`.
- **A musl-only tarball is no longer fetched, extracted or staged.** pnpm's
  `libc:` field fell through the parser's ignoring branch, so a `libc: [musl]`
  package was selected on glibc linux like any other. The parser carries `libc`
  alongside `os` and `cpu`, and selection rejects a package whose `libc` does
  not include the target's. Following npm's own `checkPlatform`, darwin and
  Windows reject such a package too. Measured here: `oxlint_linux-x64-musl`
  went from 6 occurrences in `MODULE.bazel.lock` to 0.
- **The dev server serves its own workspace on macOS.** `server.fs.allow` now
  carries both the resolved and the unresolved workspace path. Vite matches a
  request against the resolved one, so on a host where the workspace sits under
  a symlink (`/var` is `/private/var`) the server answered `403` for every
  module.
- **The generated IDE tsconfig no longer differs per host.**
  `ts_refresh_tsconfig` gains `host_only_packages`, and the workspace's own
  target names `fsevents` in it. pnpm resolves an `optionalDependencies` entry
  only where its `os`/`cpu` match, so `fsevents` — which ships `fsevents.d.ts`
  and survived the drop of packages with no declarations — put a `paths` entry
  in a checked-in file and `//:refresh_tsconfig_test` failed for everyone on the
  other platform.
- **A consumer on rules_rust 0.73 or newer can build again.** 0.73 refuses a
  `crate_universe` hub declared by a non-root module unless the rendering is
  pinned by a checked-in `lockfile`. Both hubs now ship one:
  `oxc_cli/Cargo.Bazel.lock` for `@crates` and `oj/Cargo.Bazel.lock` for
  `@oj_crates`, each beside the `Cargo.lock` that pins its cargo resolution.
  Without it, every consumer, example and integration test failed at analysis
  with "is in a non-root module but has no lockfile". Repinning is documented
  in
  [CONTRIBUTING.md](https://github.com/mikn/rules_typescript/blob/main/CONTRIBUTING.md#repinning-crate-dependencies).
- **An npm package's declaration entry point is resolved the way a resolver
  resolves it.** `_exports_types` read `exports["."]` for a `types` key directly
  under it, with no fallback to top-level `types`/`typings` — where most of npm
  publishes, **every `@types/*` package included**. The tsconfig aspect wrote a
  `paths` entry pointing at a directory, TypeScript resolved nothing, and the
  build stayed green. Resolution now walks the `exports` subtree in the map's
  own key order, array fallbacks and conditions-only shorthand included; a leaf
  naming `.js`/`.mjs`/`.cjs` resolves to the declaration beside it; then
  top-level `types`, then `typings`, extensionless included. Every candidate is
  existence-checked against the extracted package: six `@babel/helper-*`
  resolutions here name a `.d.ts` the package does not ship.
- An `@types/*` dep supplies its ambient globals. The entry-point `.d.ts` of
  each direct `@types/*` dep is listed in the generated tsconfig's `files`, and
  no `typeRoots` is derived at all. The derived `typeRoots` named the package
  directory itself and `external/`, so `process`, `setTimeout`, `Buffer` and
  `import 'node:fs'` were all errors. `NpmPackageInfo` gains
  `ambient_types_file`; `TsDeclarationInfo.type_roots` is deleted.
- Consumer setup no longer asks for
  `build --@rules_rust//rust/toolchain/channel=stable`. The flag is a
  consumer-visible error (`No repository visible as '@rules_rust'`) and a no-op
  here; this ruleset's Rust channel already defaults to stable.
- Install instructions document the pre-BCR `git_override` /
  `archive_override` / `local_path_override` recipes, since no registry entry or
  release exists for a bare `bazel_dep` to resolve against.
- Cargo build output is gitignored. `oxc_cli/target/` had 2322 tracked files,
  622 MiB of blobs, which made the release `git archive` of HEAD 206 MiB, not
  the 1.2 MiB it is now.
- Gazelle parses `tsconfig.json` as JSONC: comments and trailing commas no
  longer make `compilerOptions.paths` silently disappear.
- Gazelle follows `extends`, as a single specifier or an array of them, so a
  workspace member whose `tsconfig.json` only inherits a shared base still gets
  that base's `paths` and `baseUrl`. The merge is `tsc`'s: a key is replaced
  wholesale by the config nearest the leaf, never merged entry by entry, and a
  relative target resolves against the directory of the config that wrote it.
  A specifier that resolves through `node_modules` is skipped with a warning.
  A stub `tsconfig.json` that only extends therefore stops being inert, and a
  `tsconfig.json` with `paths` replaces the alias map for its directory and
  everything below: a subtree that reached a parent's `# gazelle:ts_path_alias`
  keys through such a stub now keeps only what the base declares.
- Gazelle picks a `compilerOptions.paths` fallback entry that exists, no longer
  always the first. Entries under the `bazel-*` convenience symlinks are dropped
  outright (`ts_compile` fails analysis on an alias pointing into the output
  tree), as are dot-directory entries such as `.bazel/npm`; of what is left, the
  first that exists on disk becomes the alias, and when none do the first is
  kept. The
  `paths entry "…" has N targets; using only "…" (first)` line — 74 per run on
  this repository, all the `./bazel-bin/…` mirror `ts_refresh_tsconfig` writes —
  is replaced by one that fires only when two entries in a chain are both real
  directories.
  `path_aliases` is unchanged: still `string_dict`, still one directory per
  alias.
- Gazelle names `css_library`, `css_module`, `asset_library` and `json_library`
  targets after the whole filename (`button.css` → `button_css`), so they no
  longer collide with the directory-named `ts_compile` target or each other.
- Integration runners no longer default their scratch directory to `/tmp`: a
  nested Bazel output base does not fit in a small tmpfs, and the failure
  surfaced as "No space left on device".
- The tsgo repository rule no longer runs an unchecked host `chmod +x`; the exec
  bit comes from the npm tarball.
- `ts_npm_publish` no longer creates an undeclared `package` symlink next to its
  output; two publish targets in one package wrote that path, so a concurrent
  build could archive the wrong directory. The staging directory is
  itself named `package` and `tar -C` reads it directly.
- `ts_npm_publish`'s staging action runs under `set -euo pipefail`; a failed
  copy used to leave a partial package and exit 0.
- `ts_binary` with a Vite bundler no longer fails analysis with "No attribute
  'env_vars' in attr". The Vite config generator reads the bundle attrs
  `ts_binary` does not declare through their defaults.
- Rules that run Node inside a build action (`css_module`, `json_library`,
  `ts_codegen`, `next_build`, `ts_npm_publish`, `vite_bundler`) resolve it from
  the `js_tool` (exec platform) toolchain, no longer from `js_runtime` (target
  platform). Under a `--platforms` that differs from the host they ran the
  target's Node: `node.exe` on a Linux builder.
- `vite_bundler` and `next_build` no longer fall back to a `node` from `PATH`
  when no toolchain is registered; the missing toolchain is an error.
- `//vite:vite_plugin_bazel` is built by a rule that resolves the Node
  toolchain, replacing a genrule whose hand-written `config_setting`s keyed the
  target platform to pick an exec-platform `@nodejs_*` binary. It failed
  analysis under `--platforms=//platforms:windows_amd64`.
- An npm tarball's top-level directory is detected with `rctx.path().readdir()`,
  no longer by shelling out to the host `tar`, which on failure returned `""`
  and fell back to `package`, aborting the fetch for DefinitelyTyped-style
  tarballs such as `@types/express-serve-static-core` (packed under
  `express-serve-static-core v4.19/`). No `rctx.execute` call remains in the
  ruleset.
- Launchers resolve paths through the Bazel runfiles library, so they work with
  a runfiles **manifest** and no symlink tree; the hand-rolled
  `RUNFILES_DIR`/`TEST_SRCDIR`/`$0.runfiles` discovery died at `cd` there. Four
  hand-written shell-quoting helpers are deleted with it.
- Cycle breaking removes only cycle-closing edges. Self-edges are now removed (a
  one-member SCC broke nothing, so Bazel saw the cycle), and an edge between two
  distinct cycles is kept. Every edge a hub drops is written into
  `MODULE.bazel.lock` as `broken_cycle_edges`: four in `@npm`, three in
  `@npm_workers`, one in `@npm_eslint`, none in the other three hubs.
- `ts_dev_server` no longer falls back to a host-PATH `node` or `vite`. A
  missing JS runtime toolchain fails at analysis time; a missing
  `node_modules`/vite fails the launcher with an actionable message.
- The Vite HMR watcher rides Vite's own `server.watcher`, no longer
  `import('chokidar')` — chokidar is inlined into Vite's dist chunk, so that
  import always threw `ERR_MODULE_NOT_FOUND` and every rebuild was dropped
  silently. A watcher that cannot start now warns.
- The Vite bundler creates a `node_modules` beside the generated config.
  `linux-sandbox` remounts `/` read-only, so Vite's `.vite-temp` mkdir hit the
  source tree with `EROFS`, which Node's `recursive: true` mkdir masks as
  `ENOENT` while Vite tolerates only `EACCES`. `examples/tanstack-app` could not
  build.
- Gazelle names a framework bundle's node_modules tree `node_modules`. Node
  realpaths a module before resolving its bare imports, so `<app>_node_modules`
  made every Vite framework bundle fail to find `rolldown`.
- No action needs a shell on the exec platform: `ctx.actions.run_shell` is gone
  from the ruleset.
- Gazelle's path-alias resolution is deterministic. When several
  `compilerOptions.paths` entries match one specifier — a tsconfig declaring
  both `@x` and `@x/*` — the longest matching alias key wins, matching
  TypeScript's own resolution. Go's randomised map iteration used to pick, so
  two identical runs could disagree. Ties break lexicographically.
- An alias key without a trailing slash matches only at a path-segment boundary:
  `@shared` no longer swallows `@sharedX`. Gazelle's alias test is then
  identical to the strict-deps check's.
- Gazelle's import scanner is a character-walk lexer, the same walk the
  strict-deps check runs. The regex set it replaces missed `import def, * as ns
  from "x"` (no dep generated) and matched specifiers inside template literals
  (deps on labels that do not exist).
- **Gazelle resolves a specifier that spells out its extension.** A
  NodeNext-style `./rules/foo.js` over a `foo.ts` source got a dep on
  `//<dir>/foo.js`, a label that does not exist. One candidate list now serves
  both sides: the path as written, the path with its extension dropped, that
  stem under each known extension, and `<stem>/index.ts[x]`.
- **Gazelle no longer writes a dep for a Node built-in spelled without
  `node:`.** `import { join } from "path"` became `@npm//:path`, which does not
  exist. The exemption matches the strict-deps checker's on the bare name.
- **A `ts_compile`'s `module_name` is indexed, and a bare specifier consults the
  index before npm.** `import "@acme/lib"` became `@npm//:acme_lib`, and the hub
  has no such package.
- **A generated `ts_compile` carries only the path aliases it can satisfy.**
  The whole workspace `paths` map was copied onto every target, and `ts_compile`
  hard-fails on an alias whose files are none of its inputs, so a Gazelle run
  could leave dozens of targets failing analysis. A target gets the aliases its
  own imports match, plus any alias whose directory holds its own sources.
  Aliases read back out of a tsconfig this ruleset generated are an echo and are
  skipped; a `# gazelle:ts_path_alias` directive reaches the IDE tsconfig's
  `paths` even when nothing imports through it yet.
- **A generated target no longer claims a hand-written target's srcs.** Two
  `ts_compile`s over one source declare the same `.js` and `.d.ts`, a
  conflicting-action error. Generation drops any src an existing
  `ts_compile`/`ts_test`/`css_*`/`asset_library`/`json_library` in the same
  BUILD file already lists, plus any `ts_test`'s `setup_files` and
  `global_setup`.
- **Gazelle no longer emits a dep on the importing package itself.** A module in
  the importer's own package that no rule claims used to resolve to that
  package's own label, a dependency cycle. It resolves to nothing, and an
  unindexed module elsewhere resolves to its directory's target when the last
  segment names a file.
- **`bazel run //gazelle` is a no-op on a clean checkout of this repository:
  zero files changed.** Ten BUILD files came back modified because they differed
  from Gazelle's own rendering, not because anything had drifted. The fixtures
  carry that rendering, and the hand-written forms that must survive a run are
  pinned with `# keep` — `visibility` included, since it merges and a
  hand-narrowed one came back `//visibility:public` every run. Check yours with
  `bazel run //:gazelle -- -mode=diff`.
- **Gazelle names the framework it will not bundle.** SvelteKit and Solid Start
  were emitting `node_modules` + `vite_bundler` + `ts_bundle` targets that
  cannot build: SvelteKit's plugin runs its own sync step from the Vite `config`
  hook and wants files `staging_srcs` cannot carry, and `@solidjs/start` ships
  no Vite plugin at all, so `defineConfig()` returns a vinxi app the
  `vite_config` contract discards. Both are still detected. SvelteKit now routes
  to `sveltekit_build`, which owns the working directory its plugin reads. Solid
  Start writes no bundle target and logs the framework, the reason and the
  fallback; a detected framework in neither table logs that no reason is
  registered for it.
- **The generated Remix bundle builds.** Its `entry_point` was
  `":entry_client"`, root-package-relative, and the root has no TypeScript, so
  the label dangled and killed `bazel build //...` for the whole workspace. It
  names `//app:entry_client`, and `package.json` is staged for the
  `@remix-run/dev` plugin to read from the staging root.
- `bazel run //path:my_test.update_snapshots` no longer writes a `.vite/` cache
  directory into the source tree. Update mode runs with the working directory at
  `BUILD_WORKSPACE_DIRECTORY`, from which vite derived `cacheDir`, so the layer
  pins `cacheDir` under the target's output directory.

### Measurements

Both numbers below come from one machine on one day; only the first can still
be reproduced from this tree.

- Declaration emitters, via `tools/bench_declarations.sh 20 50 3` (1,000
  annotated files, 20 packages, one linear chain, medians of three interleaved
  runs): `declarations = "tsgo"` 6.3s wall / 4.89s critical path,
  `declarations = "oxc"` 3.8s / 2.15s, `"oxc"` with `enable_check = False`
  2.7s / 1.06s. The script is committed; re-run it on your own graph.
- npm layout, measured while both existed: building one vitest target from an
  empty output base against a 2731-package lockfile went from 392s / 2.9 GB of
  `external/` to 66s / 415 MB, fetching 138 packages (vitest's actual transitive
  closure) and not all 2731. The single-repository arm is deleted, so this is a
  historical record. What is reproducible:
  `bazel query 'kind(ts_npm_package, deps(//your:test))'` counts the package
  targets a target can reach; here `//tests/vitest/environment:node_test`, whose
  only npm dep is `@npm//:vitest`, reaches 94, one repository each.

### Deliberate behaviour

- **Ambient globals reach a target from its direct `@types/*` deps only.**
  Declaring `@types/node` is how a target asks for `process`. Transitive
  `@types` declarations still arrive as inputs; they are not named in the
  tsconfig's `files`; `# gazelle:ts_ambient_types` covers the ergonomics. The
  editor's single root program unions every ambient entry in the
  graph, so an undeclared global type-checks in the editor and fails the build;
  narrowing that per target would need a tsconfig per target. `bazel build` is
  the authority, per
  [ide-setup.md](https://mikn.github.io/rules_typescript/getting-started/ide-setup/).
- **`skipLibCheck: true` stays in the zero-config baseline.** tsgo over the
  Workers fixture, three ways: with the rule's `lib = ["es2022"]`, zero errors;
  without `lib` and `skipLibCheck: true`, one error at the use site
  (`TS2339: Property 'default' does not exist on type 'CacheStorage'`); without
  `lib` and `skipLibCheck: false`, that same error plus two masked `TS2403`
  declaration conflicts. Narrowing `lib` is the fix, a documented `ts_compile`
  attribute with a worked Cloudflare example.
- **The array `config` form of `ts_test` requires vitest 3.2 or later.** vitest
  4 removed `test.workspace` and throws on it; vitest below 3.2 does not know
  `test.projects`. The generated config picks what the tested lane requires.
  Supporting 3.0/3.1 would mean sniffing vitest's version at config-load time
  plus a second lockfile lane, which COMPATIBILITY.md rules out by policy. No
  lockfile here resolves vitest 3; what is true of the other `config` shapes is
  that they use no version-sensitive key.
- **`jsdom` and `edge-runtime` stay analysis-only.** `environment` is a string
  the rule forwards to `test.environment`, and nothing in `ts_test` branches on
  its value, so `dom` and `node` pin both sides of the only per-value axis:
  `resolve.preserveSymlinks`; `//tests/workers` covers the third case, where a
  pool wants it false and the user layer wins. In tree the two absent values
  would cost a fifth lockfile pinning Vite and vitest, and jsdom also couples to
  `node_version` (jsdom 30 wants node `^22.22.2`; the toolchain is 22.14.0, so
  the pin would sit at 29) plus 126 lines of declaration closure in the
  checked-in `tsconfig.json`. Both stay `manual` behind a `build_test`, and
  `MANUAL_ONLY` in `tools/ci/check_test_sources.sh` names both files and is
  exact in both directions, so untagging either fails CI.
- **`/// <reference types="x" />` is not checked.** The directive resolves
  through TypeScript's type-reference resolver (`node_modules/@types` and
  `typeRoots`), not the `paths` map that carries npm deps, and there is no
  `node_modules` to walk, so it cannot resolve however the target is declared.
  tsgo reports `TS2688: Cannot find type definition file for 'x'`. Gazelle
  cannot name a label either: `types="x"` means `@types/x` or `x`. The remedy is
  the rule: `vite_types = True`, or an ordinary `@types/*` dep.

### Known gaps

- **A `ts_codegen` generator that emits `.ts` sources into an `out_dir` has no
  route to a `ts_compile`.** `deps` takes the tree as already-compiled output
  and nothing downstream compiles it; `srcs` cannot take it, because one output
  per input file is declared at analysis time. Closing this needs an oxc
  invocation over a whole directory emitting a directory, which is a different
  action shape, not an attribute.
- **`ts_codegen(node_modules = ...)` only serves an ESM generator when the
  target is named literally `node_modules`.** The tree's directory is named
  after its target and Node's ESM resolver only ever looks in a directory called
  `node_modules` as it walks up; `NODE_PATH`, which the rule sets, is a CJS
  mechanism. A misnamed target leaves the generator failing with
  `ERR_MODULE_NOT_FOUND`. Documented rather than fixed: renaming the artifact
  under the target would break the `node_modules()` rule's own contract.
- **Three `@npm` labels remain in ruleset packages, unreached.**
  `//vite:plugin_typecheck` (`@npm//:types_node`, `@npm//:vite`),
  `//vite:tsup_config` (`@npm//:tsup`) and `//ts/private/css:compiler_typecheck`
  (`@npm//:types_node`). `rdeps` says nothing consumer-facing reaches them.
  Making the invariant absolute means either adding
  `@types/node`, `vite` and `tsup` to the non-dev hub, growing every consumer's
  `MODULE.bazel.lock` for targets no consumer builds, or moving the targets into
  packages consumers never load, which needs `ts_compile` srcs to cross a
  package boundary.
- **`_short_digest` is Java's `String.hashCode` masked to 32 bits.** Two peer
  suffixes agreeing on their first 40 sanitised characters and colliding on that
  hash merge into one repository and one store directory. Starlark has no real
  hash function. A fix keyed on the hub's enumerated snapshot dict was rejected:
  it broke `//tests/integration:tanstack_test` with a dangling link to
  `tiny-invariant`. Cross-hub is a second case: `_store_path` and `_package_key`
  in `ts/private/node_modules.bzl` carry no hub component.
- **There is still no libc `constraint_setting`.** Selection no longer needs
  one, since `libc:` is honoured in the parser and the matcher. What one would
  add is the ability to register a musl toolchain, and Node.js publishes no
  official musl tarball to register.
- **A `compilerOptions.paths` chain collapses to one directory.** Gazelle picks
  the first entry that exists on disk. Of 448 `paths` keys in this repository's
  own root tsconfig, 78 are chains and not one has a second entry surviving the
  `bazel-*` filter. A key with two genuinely distinct real directories keeps one
  and logs which it used and which it dropped; a specifier only the dropped
  directory provides fails with `TS2307`. Widening `path_aliases` to a
  `string_list_dict` was rejected: `_validate_path_aliases` requires every alias
  directory to hold one of that target's own staged inputs and Gazelle emits no
  `path_alias_srcs`, and `TsconfigSourcesInfo.aliases` carries no ordering index
  for the workspace-root tsconfig's union.
- **`vite/bundler.bzl` borrows the name `node_modules` in the package output
  directory, which a sibling `node_modules()` target may already own.** Under
  the default sandbox the wrapper's `ln -sf` plants the name fresh and two Vite
  majors in one package do build. With sandboxing off
  (`--spawn_strategy=local`) the link lands inside the sibling's declared output
  and the action runs the sibling's Vite, silently. Nesting each tree at
  `<target>/node_modules` does not fix it: the generated config sits outside the
  tree and its `import { defineConfig } from "vite"` resolves by walking up out
  of it. Order of work if taken on: drop that import (`defineConfig` is an
  identity function, and `ts_dev_server` already loads its plugin by absolute
  path), then the link, then the tree layout. Until then, one tree per Bazel
  package, or one per package per Vite major.
- **Windows is unsupported**, not partially supported. See
  [COMPATIBILITY.md](https://github.com/mikn/rules_typescript/blob/main/COMPATIBILITY.md#platforms).

## [0.1.0] — never released

No tag, no release and no registry entry exists for this version. The list
below records what the ruleset did before the changes above.

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
