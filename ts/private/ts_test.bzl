"""Test rule (macro) that compiles and runs vitest in Bazel's test sandbox.

NOTE — Windows compatibility:
  The node_modules tree action (_ts_auto_node_modules) runs via a cross-platform
  Node.js script and works on all platforms including Windows.

  However, the test runner itself (_ts_test_runner_impl / _ts_snapshot_updater)
  generates a bash script and is therefore NOT compatible with Windows.  Running
  `bazel test` or `bazel run` with ts_test targets on Windows requires a bash
  environment (e.g. Git Bash, WSL) or a future replacement of the runner script
  with a platform-independent alternative (see TODO.md Sub-Project 11.1).

ts_test is a macro that:
  1. Creates an internal ts_compile target for the test source files.
  2. Creates a _ts_test_runner_test test rule that runs vitest against the compiled
     .js outputs.

Design:
  - srcs: the .ts/.tsx test source files
  - deps: ts_compile targets that the tests import (production code)
  - node_modules: a node_modules target for runtime npm resolution
  - vitest: optional explicit label for the vitest bin

Who controls the test environment:
  The user does, through `config` (a vitest config file or an inline dict) and
  through the environment attributes (setup_files, global_setup, environment,
  globals, reporters, coverage_thresholds, data).  rules_typescript keeps only
  what Bazel must own — the CSS-module plugin for CssModuleInfo deps, npm
  resolution inside the runfiles tree, and the coverage output paths — and
  MERGES the user's config on top of it instead of being replaced by it.  The
  layering and its precedence are described under "Vitest config generation"
  below; the config that actually ran is available as the `vitest_config`
  output group of any ts_test target:

    bazel build //path:my_test --output_groups=vitest_config

The vitest runner script:
  - Changes to the runfiles directory
  - Sets NODE_PATH to point at the generated node_modules tree
  - Invokes vitest with the test .js files for the current shard
  - Exits with vitest's exit code

Test sharding: the runner distributes test files across shards using
TEST_SHARD_INDEX and TEST_TOTAL_SHARDS environment variables.

npm package naming convention:
  By default, ts_test auto-generates a node_modules tree from any deps that
  provide NpmPackageInfo (i.e. @npm// labels).  The @npm workspace name is the
  conventional name used by rules_js/npm_translate_lock for the npm registry.
  If your workspace uses a non-default name (e.g. @my_npm), pass it via the
  npm_workspace_name param.

  IMPORTANT: the auto node_modules tree is built from the direct deps that
  provide NpmPackageInfo, plus their transitive npm dependencies (via
  NpmPackageInfo.transitive_deps).  If your production code (non-npm deps like
  ts_compile targets) depends on npm packages that are NOT also listed in
  ts_test's deps, those packages will be missing at runtime.  The recommended
  practice is to list all npm packages needed at runtime — both by the test
  files and by the production code under test — directly in ts_test's deps.
  This mirrors how go_test works: all direct imports must be listed.

  Gazelle handles this automatically: it collects imports from both the test
  files and the production source files in the same package, and emits all
  required @npm// labels in ts_test's deps.

Snapshot testing:
  Vitest resolves a .snap file next to the test file it ran, which under Bazel
  is the compiled .js in bazel-out.  ts_test replaces that resolution
  (test.resolveSnapshotPath) with the path the .ts source implies:

    <package>/__snapshots__/<source file name>.snap

  the same place a plain `vitest` run would keep it, so a repository adopting
  ts_test keeps its snapshots where they already are.

  Reading them: list them in `snapshots`, which is what puts them in the
  runfiles tree the sandboxed test can read.  A test whose snapshot is absent
  there fails -- ts_test runs vitest in its read-only snapshot mode (CI=true)
  so that no `bazel test` can write a .snap and pass on what it just wrote.

  Writing them: every ts_test also declares an executable

    bazel run //path:my_test.update_snapshots

  which runs the same compiled tests with `vitest --update` and writes the
  files under BUILD_WORKSPACE_DIRECTORY.  It shares the test's ts_compile
  target; a second ts_test over the same srcs would collide with it on the
  compiled .js outputs.
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:node_modules.bzl", "build_node_modules_action", "collect_npm_packages")
load("//ts/private:providers.bzl", "CssModuleInfo", "JsInfo", "NpmPackageInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_runtime")
load("//ts/private:ts_compile.bzl", "fail_on_mixed_src_packages", "ts_compile")

# ─── Internal auto node_modules rule ──────────────────────────────────────────
#
# This rule accepts any deps (no provider constraint) and builds a node_modules
# tree from those deps that provide NpmPackageInfo.  It is used by the ts_test
# macro to handle the case where the caller passes both @npm// labels AND
# ts_compile targets in deps — the rule silently skips non-npm deps.

def _ts_auto_node_modules_impl(ctx):
    packages_to_link = collect_npm_packages([
        dep[NpmPackageInfo]
        for dep in ctx.attr.deps
        if NpmPackageInfo in dep
    ])

    input_file_sets = [npm_info.all_files for npm_info in packages_to_link]

    # Delegate to the shared cross-platform action helper from node_modules.bzl.
    # When the JS tool toolchain is available (which it always is here,
    # since _ts_auto_node_modules is only used inside ts_test which requires
    # Node), the action uses Node.js and works on Windows.
    #
    # The leaf must be literally "node_modules" for Node's ESM parent-directory
    # walk; the target-named directory above it keeps two ts_test targets in one
    # package from declaring the same output.
    out_dir = build_node_modules_action(
        ctx,
        packages_to_link,
        input_file_sets,
        output_name = "{}/node_modules".format(ctx.label.name),
    )

    return [
        DefaultInfo(
            files = depset([out_dir]),
            runfiles = ctx.runfiles(files = [out_dir]),
        ),
    ]

_ts_auto_node_modules = rule(
    implementation = _ts_auto_node_modules_impl,
    attrs = {
        "deps": attr.label_list(
            doc = "Any deps; those not providing NpmPackageInfo are silently skipped.",
        ),
    },
    toolchains = [
        # The tree is built by a build action, so the Node that builds it is the
        # exec-platform tool, not the runtime the test itself executes on.
        # mandatory = True: _ts_auto_node_modules is only created inside the ts_test
        # macro, which always requires a Node.js runtime.  Requiring the toolchain
        # prevents silent fallback to the bash path on misconfigured setups.
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = True),
    ],
    doc = "Internal rule: builds a node_modules tree from any deps that provide NpmPackageInfo.",
)

# ─── Vitest config generation ────────────────────────────────────────────────
#
# ts_test always generates ONE vitest config and passes it with --config, so
# vitest never auto-discovers a stray config from the runfiles tree.  That
# generated file is an *entry* config that layers three sources, lowest
# precedence first:
#
#   1. the Bazel layer   — machinery rules_typescript owns (the CSS-module
#                          plugin when a dep provides CssModuleInfo)
#   2. the user layer    — the `config` attr: either a vitest config file
#                          (.ts/.mts/.js/.mjs) or an inline dict
#   3. the attribute layer — `environment`, `setup_files`, `global_setup`,
#                          `globals`, `reporters`, `coverage_thresholds`
#   4. the snapshot layer  — where .snap files are read from and written to.
#                          Merged at the root only: vitest rejects
#                          `resolveSnapshotPath` in a project entry.
#
# Objects are merged key by key; arrays are concatenated (base first), which
# matches vite's own mergeConfig, so a user `plugins` list never displaces the
# CSS-module plugin and a user `setupFiles` list never displaces `setup_files`.
# Scalars from a later layer win.
#
# Bazel's coverage output wiring stays on the vitest command line, where it
# outranks every layer: `bazel coverage` must write lcov where Bazel expects it.

_SNAPSHOT_HELPERS = """\
const snapshotBase = (testPath) => {
  const p = testPath.split(sep).join('/');
  for (const [compiled, base] of Object.entries(SNAPSHOT_BASES)) {
    if (p === compiled || p.endsWith('/' + compiled)) return base;
  }
  return null;
};

// The test vitest runs is the compiled .js in bazel-out, so vitest's own
// answer -- a __snapshots__ dir beside the test file -- points at the build
// tree. Every snapshot path below is rebuilt from the .ts source instead.
const vitestDefaultSnapshotPath = (testPath, ext) =>
  join(dirname(testPath), '__snapshots__', basename(testPath) + ext);

let RUNFILES_MANIFEST;
const rlocation = (p) => {
  const dir = process.env.RUNFILES_DIR;
  if (dir) return resolve(dir, p);
  if (RUNFILES_MANIFEST === undefined) {
    const manifest = process.env.RUNFILES_MANIFEST_FILE;
    RUNFILES_MANIFEST = new Map(
      (manifest ? readFileSync(manifest, 'utf8').split('\\n') : [])
        .filter((line) => line.includes(' '))
        .map((line) => [line.slice(0, line.indexOf(' ')), line.slice(line.indexOf(' ') + 1)]),
    );
  }
  return RUNFILES_MANIFEST.get(p) ?? abs(p);
};
"""

_CONFIG_MERGE_HELPERS = """\
const isPlainObject = (v) =>
  v !== null && typeof v === 'object' && !Array.isArray(v) && !(v instanceof RegExp);

// Key-by-key merge with array concatenation, mirroring vite's mergeConfig.
const merge = (a, b) => {
  if (b === undefined) return a;
  if (!isPlainObject(a) || !isPlainObject(b)) return b;
  const out = { ...a };
  for (const [k, v] of Object.entries(b)) {
    if (Array.isArray(out[k]) && Array.isArray(v)) out[k] = [...out[k], ...v];
    else out[k] = merge(out[k], v);
  }
  return out;
};
"""

def _js(value):
    """Renders a Starlark value as a JS literal."""
    return json.encode(value)

def _js_scalar(s):
    """Renders a string attr value as a JS number/boolean when it looks like one."""
    if s in ("true", "false"):
        return s
    digits = s[1:] if s.startswith("-") else s
    digits = digits.replace(".", "", 1)
    if digits and not digits.strip("0123456789"):
        return s
    return json.encode(s)

def _relative_import(from_path, to_path):
    """Relative path from the directory holding from_path to to_path.

    Both arguments are runfiles-relative paths, so the result is valid from the
    generated config wherever the runfiles tree is materialised.
    """
    from_dir = from_path.split("/")[:-1]
    to_parts = to_path.split("/")
    shared = 0
    for i in range(min(len(from_dir), len(to_parts) - 1)):
        if from_dir[i] != to_parts[i]:
            break
        shared = i + 1
    parts = [".."] * (len(from_dir) - shared) + to_parts[shared:]
    joined = "/".join(parts)
    return joined if joined.startswith("..") else "./" + joined

def _snapshot_layer(snapshot_bases, snapshot_root, test_include, update_snapshots):
    """The root-only config layer that redirects vitest's .snap paths.

    `resolveSnapshotPath` is one of vitest's non-project options, so this layer
    is merged at the root and never into a `test.projects` entry.
    """
    if not snapshot_bases:
        return "const snapshotLayer = {};"
    if update_snapshots:
        return "\n".join([
            "const SOURCE_ROOT = process.env.BUILD_WORKSPACE_DIRECTORY;",
            "const snapshotLayer = {",
            # vite derives cacheDir from the root, and `bazel run` puts the root
            # in the user's source tree, where a .vite/ has no business being.
            "  cacheDir: abs('.vitest-cache'),",
            "  test: {",
            # `bazel run` puts the working directory at the workspace root, and
            # vitest globs that for test files. The compiled ones are here, and
            # naming them keeps the runfiles trees beside them out of the run.
            "    dir: abs('.'),",
            "    include: {},".format(_js(test_include)),
            "    resolveSnapshotPath: (testPath, ext) => {",
            "      const base = snapshotBase(testPath);",
            "      if (base === null || !SOURCE_ROOT) return vitestDefaultSnapshotPath(testPath, ext);",
            "      return resolve(SOURCE_ROOT, base + ext);",
            "    },",
            "  },",
            "};",
        ])
    return "\n".join([
        "const snapshotLayer = {",
        "  test: {",
        "    resolveSnapshotPath: (testPath, ext) => {",
        "      const base = snapshotBase(testPath);",
        "      if (base === null) return vitestDefaultSnapshotPath(testPath, ext);",
        "      return rlocation({prefix} + base + ext);".format(prefix = _js(snapshot_root + "/")),
        "    },",
        "  },",
        "};",
    ])

def _vitest_config_content(
        config_rf,
        css_module_plugin_rf,
        user_config_rf,
        user_config_json,
        environment,
        setup_files_rf,
        global_setup_rf,
        globals_enabled,
        reporters,
        coverage_thresholds,
        coverage_provider,
        snapshot_bases = {},
        snapshot_root = "",
        test_include = [],
        update_snapshots = False):
    """Builds the entry vitest config that layers Bazel, user and attr config."""
    lines = [
        "// AUTO-GENERATED by rules_typescript ts_test. Do not edit.",
        "//",
        "// Layers, lowest precedence first: Bazel machinery, the `config` attr,",
        "// then the ts_test attributes, then the snapshot layer.",
        "// Arrays concatenate; scalars are overridden.",
        "import { " +
        ("basename, dirname, join, resolve, sep" if snapshot_bases else "dirname, resolve") +
        " } from 'node:path';",
        "import { fileURLToPath } from 'node:url';",
    ]
    if snapshot_bases:
        lines.append("import { readFileSync } from 'node:fs';")
    if css_module_plugin_rf:
        # The plugin that answers a *.module.css import with the export map
        # css_module wrote, so a test reads the class name the browser gets.
        lines.append("import {{ cssModulesTestPlugin }} from '{}';".format(
            _relative_import(config_rf, css_module_plugin_rf),
        ))
    if user_config_rf:
        lines.append("import userConfigExport from '{}';".format(
            _relative_import(config_rf, user_config_rf),
        ))
    lines += [
        "",
        "const HERE = dirname(fileURLToPath(import.meta.url));",
        "const abs = (p) => resolve(HERE, p);",
        "",
    ]
    if snapshot_bases:
        lines += [
            "const SNAPSHOT_BASES = {};".format(_js(snapshot_bases)),
            _SNAPSHOT_HELPERS,
        ]

    base_plugins = ["cssModulesTestPlugin()"] if css_module_plugin_rf else []

    lines += [
        # Every path vitest is handed is a runfiles symlink; resolving them to
        # their targets walks out of the test sandbox, which the browser-like
        # environments do and the node one does not.
        #
        # A pool that resolves modules for a second runtime is the other case,
        # and it wants the opposite: under @cloudflare/vitest-pool-workers a
        # lexical path is a second module identity for the same file, so a
        # workers config sets resolve.preserveSymlinks = false and the user layer
        # wins. See //tests/workers:vitest.workers.config.mjs.

        # A file under test is a build output, so its realpath lies outside the
        # vite root -- which the coverage default drops before instrumenting.

        # vite's default root is the working directory, which for a test is the
        # runfiles root. A config author writes paths relative to their package,
        # and everything that resolves against the root -- the Workers pool's
        # `wrangler.configPath` among them -- then looks a whole tree too high.
        # It is the launcher's runfiles-resolved path and not this file's own
        # dirname because import.meta.url comes back as the bazel-out realpath,
        # which no runfiles path is under.
        "const bazelLayer = {",
        "  root: process.env.TS_TEST_PACKAGE_DIR,",
        "  resolve: { preserveSymlinks: true },",
        "  plugins: [{}],".format(", ".join(base_plugins)),
        "  test: { coverage: { allowExternal: true } },",
        "};",
    ]

    if user_config_json:
        lines.append("const userConfigExport = {};".format(user_config_json))

    test_overrides = []
    if environment:
        test_overrides.append("  environment: {},".format(_js(environment)))
    if setup_files_rf:
        test_overrides.append("  setupFiles: [{}],".format(", ".join([
            "abs({})".format(_js(_relative_import(config_rf, p)))
            for p in setup_files_rf
        ])))
    if global_setup_rf:
        test_overrides.append("  globalSetup: [{}],".format(", ".join([
            "abs({})".format(_js(_relative_import(config_rf, p)))
            for p in global_setup_rf
        ])))
    if globals_enabled:
        test_overrides.append("  globals: true,")
    if reporters:
        test_overrides.append("  reporters: {},".format(_js(reporters)))
    coverage_keys = []
    if coverage_provider:
        coverage_keys.append("provider: {}".format(_js(coverage_provider)))
    if coverage_thresholds:
        thresholds = ", ".join([
            "{}: {}".format(_js(k), _js_scalar(coverage_thresholds[k]))
            for k in sorted(coverage_thresholds)
        ])
        coverage_keys.append("thresholds: {{ {} }}".format(thresholds))
    if coverage_keys:
        test_overrides.append("  coverage: {{ {} }},".format(", ".join(coverage_keys)))

    if test_overrides:
        lines.append("const attrLayer = {\n test: {\n" + "\n".join(test_overrides) + "\n } };")
    else:
        lines.append("const attrLayer = {};")

    lines.append(_snapshot_layer(snapshot_bases, snapshot_root, test_include, update_snapshots))
    lines += [
        "",
        _CONFIG_MERGE_HELPERS,
        "export default async (env) => {",
    ]
    if user_config_rf or user_config_json:
        lines += [
            "  let user = typeof userConfigExport === 'function'",
            "    ? await userConfigExport(env)",
            "    : await userConfigExport;",
            "  // A config file that default-exports an array is a list of vitest",
            "  // projects; vitest 4 removed test.workspace and throws on it.",
            "  if (Array.isArray(user)) user = { test: { projects: user } };",
            "  if (!isPlainObject(user)) user = {};",
        ]
    else:
        lines.append("  const user = {};")
    lines += [
        "  const merged = merge(merge(merge(bazelLayer, user), attrLayer), snapshotLayer);",
        "  // Every project gets its own Vite server, so the Bazel layer and the",
        "  // attribute layer have to be applied to each project too.",
        "  const projects = merged.test && merged.test.projects;",
        "  if (Array.isArray(projects)) {",
        "    merged.test = {",
        "      ...merged.test,",
        "      projects: projects.map((p) =>",
        "        isPlainObject(p) ? merge(merge(bazelLayer, p), attrLayer) : p,",
        "      ),",
        "    };",
        "  }",
        "  return merged;",
        "};",
        "",
    ]
    return "\n".join(lines)

def _snapshot_bases(srcs):
    """Maps each compiled test .js to the .snap path its .ts source implies.

    Keyed by the compiled path so the generated config can match the file
    vitest reports, whatever prefix the runfiles layout gives it.
    """
    bases = {}
    for src in srcs:
        if src.extension not in ("ts", "tsx", "mts", "cts"):
            continue
        stem = src.short_path[:-(len(src.extension) + 1)]
        parent = stem[:stem.rfind("/")] if "/" in stem else ""
        bases[stem + ".js"] = "{}/__snapshots__/{}".format(parent, src.basename)
    return bases

# ─── Internal test runner rule ─────────────────────────────────────────────────

def _ts_test_runner_impl(ctx):
    # Collect transitive .js files from all deps.
    transitive_js_sets = []
    for dep in ctx.attr.deps:
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)

    transitive_js = depset(transitive = transitive_js_sets, order = "postorder")

    # The test .js files come from the compiled test target.
    test_js_files = ctx.files.compiled_tests

    # DefaultInfo also carries the .d.ts and .js.map beside every module; vitest
    # takes this list as the files to run.
    test_entry_points = [f for f in test_js_files if f.extension in ("js", "mjs", "cjs")]

    # Collect the node_modules directory.
    node_modules_files = ctx.files.node_modules

    # A *.module.css anywhere in the closure means something under test imports
    # one, which Node cannot load; the generated config answers it.  ts_compile
    # always advertises CssModuleInfo, so the depset has to be the test — the
    # provider's presence alone would install the plugin everywhere.
    needs_css_module_plugin = False
    css_module_sets = []
    for dep in ctx.attr.deps:
        if CssModuleInfo not in dep:
            continue
        info = dep[CssModuleInfo]
        css_module_sets.append(info.transitive_css_files)
        css_module_sets.append(info.transitive_exports_files)
        if not needs_css_module_plugin and info.transitive_css_files.to_list():
            needs_css_module_plugin = True

    # Resolve vitest binary.
    # When set via the `vitest` attr, the label points to an npm_bin wrapper
    # shell script that already invokes Node internally.  We must NOT prepend
    # $RUNTIME when executing it — the wrapper handles that itself.
    vitest_bin = ctx.file.vitest  # may be None
    vitest_is_npm_bin = vitest_bin != None

    # Resolve the JS runtime.
    # Priority: per-target `runtime` attr > toolchain > system node fallback.
    runtime_binary = None
    runtime_args = []
    if ctx.file.runtime:
        runtime_binary = ctx.file.runtime
    else:
        js_runtime = get_js_runtime(ctx)
        if js_runtime:
            runtime_binary = js_runtime.runtime_binary
            runtime_args = js_runtime.args_prefix

    # Write a text file listing the test .js files.
    # The runner reads this to support sharding.
    # Store runfiles-relative paths (with _main/ prefix for main-workspace files).
    test_files_list = ctx.actions.declare_file(
        "{}_test_files.txt".format(ctx.label.name),
    )
    ctx.actions.write(
        output = test_files_list,
        content = "\n".join([rlocation_path(ctx, f) for f in test_entry_points]) + "\n",
    )

    # ── Vitest config ─────────────────────────────────────────────────────────
    # One generated entry config layers Bazel machinery, the `config` attr and
    # the environment-shaping attributes.  See "Vitest config generation" above.
    vitest_config = ctx.actions.declare_file(
        "_{}_vitest.config.mjs".format(ctx.label.name),
    )
    if ctx.file.config and ctx.attr.config_json:
        fail("ts_test: `config` takes either a config file or an inline dict, not both.")

    # The generated config imports the user's config relatively, and esbuild
    # resolves that against the real file — bazel-bin, not the runfiles tree — so
    # the user's config has to exist there too.  rules_ts copies tsconfig.json
    # into bin for the same reason.
    #
    # Staged BESIDE the node_modules tree, not in the package directory: Vite
    # leaves a bare import in a config file external, so Node resolves it by
    # walking up from where that file sits, and the tree is one level deeper than
    # the package (`<nm_target>/node_modules`, so two tests in one package do not
    # collide). From the package directory that walk never reaches it, and a
    # config importing the pool it installs -- which is what a Workers config is
    # -- fails to load. As its sibling, the first directory the walk looks in is
    # the tree itself.
    user_config = None
    if ctx.file.config:
        config_basename = "_{}_vitest.user.config.{}".format(
            ctx.label.name,
            ctx.file.config.extension,
        )
        if node_modules_files:
            user_config = ctx.actions.declare_file(
                config_basename,
                sibling = node_modules_files[0],
            )
        else:
            user_config = ctx.actions.declare_file(config_basename)
        ctx.actions.expand_template(
            template = ctx.file.config,
            output = user_config,
            substitutions = {},
        )

    # A copy beside the generated config, for the reason above: vitest resolves
    # the config's imports against the real file in bin, and the path arithmetic
    # from there to another package's output tree is not the path arithmetic from
    # the runfiles tree. Two outputs of this target in one directory is.
    css_module_plugin = None
    if needs_css_module_plugin:
        css_module_plugin = ctx.actions.declare_file(
            "_{}_css_modules.mjs".format(ctx.label.name),
        )
        ctx.actions.expand_template(
            template = ctx.file._css_module_plugin,
            output = css_module_plugin,
            substitutions = {},
        )

    setup_js = [f for f in ctx.files.setup_files if f.extension in ("js", "mjs", "cjs")]
    global_setup_js = [f for f in ctx.files.global_setup if f.extension in ("js", "mjs", "cjs")]
    ctx.actions.write(
        output = vitest_config,
        content = _vitest_config_content(
            config_rf = rlocation_path(ctx, vitest_config),
            css_module_plugin_rf = rlocation_path(ctx, css_module_plugin) if css_module_plugin else None,
            user_config_rf = rlocation_path(ctx, user_config) if user_config else None,
            user_config_json = ctx.attr.config_json,
            environment = ctx.attr.environment,
            setup_files_rf = [rlocation_path(ctx, f) for f in setup_js],
            global_setup_rf = [rlocation_path(ctx, f) for f in global_setup_js],
            globals_enabled = ctx.attr.globals,
            reporters = ctx.attr.reporters,
            coverage_thresholds = ctx.attr.coverage_thresholds,
            coverage_provider = ctx.attr.coverage_provider,
            snapshot_bases = _snapshot_bases(ctx.files.srcs),
            snapshot_root = ctx.workspace_name,
            test_include = [
                _relative_import(
                    rlocation_path(ctx, vitest_config),
                    rlocation_path(ctx, f),
                ).removeprefix("./")
                for f in test_entry_points
            ],
            update_snapshots = ctx.attr.update_snapshots,
        ),
    )

    # ── Launcher config ───────────────────────────────────────────────────────
    # Every path is a runfiles path; the launcher resolves them through the
    # runfiles library, so manifest-only layouts work like symlink trees.
    vitest_cfg = {
        "config_file": rlocation_path(ctx, vitest_config),
        "test_files_list": rlocation_path(ctx, test_files_list),
        "update_snapshots": ctx.attr.update_snapshots,
        "coverage": ctx.attr.coverage,
    }
    if vitest_bin:
        vitest_cfg["vitest"] = rlocation_path(ctx, vitest_bin)

        # An npm_bin wrapper resolves its own Node; prefixing the runtime again
        # would run a launcher under a launcher.
        vitest_cfg["vitest_is_npm_bin"] = vitest_is_npm_bin
    if node_modules_files:
        vitest_cfg["node_modules"] = rlocation_path(ctx, node_modules_files[0])

        # The canonical bin entry from vitest's package.json#bin, reached inside
        # the node_modules tree artifact.
        vitest_cfg["vitest_in_tree"] = "vitest/vitest.mjs"

    # A sandboxed test must fail on a stale or missing .snap, never write one,
    # and vitest only stops writing when it believes it is running in CI.
    env = dict(ctx.attr.env)
    if not ctx.attr.update_snapshots:
        env.setdefault("CI", "true")

    config = {
        "label": str(ctx.label),
        "mode": "vitest",
        "workspace": ctx.workspace_name,
        "runtime_args": runtime_args,
        "env": env,
        "vitest": vitest_cfg,
    }
    if runtime_binary:
        config["runtime"] = rlocation_path(ctx, runtime_binary)

    launcher = declare_launcher(ctx, config, basename = "{}_test_launcher".format(ctx.label.name))

    # Build runfiles.
    runfiles_files = (
        [test_files_list, vitest_config] + launcher.files +
        test_js_files +
        node_modules_files +
        ctx.files.setup_files +
        ctx.files.global_setup +
        ctx.files.data +
        ctx.files.snapshots
    )
    if vitest_bin:
        runfiles_files.append(vitest_bin)
    if runtime_binary:
        runfiles_files.append(runtime_binary)
    if user_config:
        runfiles_files.append(user_config)
    if css_module_plugin:
        runfiles_files.append(css_module_plugin)

    runfiles = ctx.runfiles(
        files = runfiles_files,
        # The stylesheets as well as the .js: the plugin above answers a
        # *.module.css import out of the export map beside it, which is only in
        # the sandbox because it is named here.
        transitive_files = depset(transitive = [transitive_js] + css_module_sets),
        root_symlinks = launcher.root_symlinks,
    )
    for target in ctx.attr.data + ctx.attr.setup_files + ctx.attr.global_setup:
        runfiles = runfiles.merge(target[DefaultInfo].default_runfiles)

    # An npm_bin vitest is itself launcher-driven, so it needs its own config
    # staged in this test's runfiles, not just its executable.
    if ctx.attr.vitest:
        runfiles = runfiles.merge(ctx.attr.vitest[DefaultInfo].default_runfiles)

    return [
        DefaultInfo(
            executable = launcher.executable,
            runfiles = runfiles,
        ),
        # Exposes the config vitest actually ran with, for debugging and for
        # tests that pin the layering.
        OutputGroupInfo(vitest_config = depset([vitest_config])),
        # `srcs` is not a source attribute here: the same files reach the
        # collection through the ts_compile in `compiled_tests`, which answers
        # to the filter under its own label.
        coverage_common.instrumented_files_info(
            ctx,
            dependency_attributes = ["compiled_tests", "deps"],
            extensions = _INSTRUMENTED_EXTENSIONS,
            baseline_coverage_files = [],
        ),
    ]

# ─── Coverage instrumentation ─────────────────────────────────────────────────
#
# --instrumentation_filter is applied where a target answers for its own label,
# so a dep has to build its own InstrumentedFilesInfo: collecting every `srcs`
# in the test rule would report a filtered-out library as instrumented.  What
# Bazel selects reaches the runner as COVERAGE_MANIFEST, and the runner keeps
# only those files in the lcov it hands back.
#
# baseline_coverage_files is empty on purpose: it would name the .ts a target
# declared, and the runner reports on the .js compiled from it, so the baseline
# record would be a second name for the same code, carrying no lines at all.
_INSTRUMENTED_EXTENSIONS = ["ts", "tsx", "mts", "cts", "js", "jsx", "mjs", "cjs"]

def _instrumented_files_aspect_impl(_target, ctx):
    return coverage_common.instrumented_files_info(
        ctx,
        source_attributes = ["srcs"] if hasattr(ctx.rule.attr, "srcs") else [],
        dependency_attributes = ["deps"] if hasattr(ctx.rule.attr, "deps") else [],
        extensions = _INSTRUMENTED_EXTENSIONS,
        baseline_coverage_files = [],
    )

_instrumented_files_aspect = aspect(
    implementation = _instrumented_files_aspect_impl,
    attr_aspects = ["deps"],
    doc = "Internal: gives every target under test an InstrumentedFilesInfo of its own.",
)

# Shared attribute dict for both the test and executable runner variants.
_RUNNER_ATTRS = {
    "compiled_tests": attr.label_list(
        aspects = [_instrumented_files_aspect],
        doc = "Label of the ts_compile target containing compiled test .js files.",
        allow_files = [".js"],
    ),
    "deps": attr.label_list(
        aspects = [_instrumented_files_aspect],
        doc = "ts_compile and other targets whose .js files may be available at test runtime. " +
              "Deps that do not provide JsInfo (e.g. css_module, asset_library) are silently " +
              "skipped when collecting transitive .js files.",
    ),
    "node_modules": attr.label(
        doc = "A node_modules target providing the runtime npm dependency tree.",
        allow_files = True,
    ),
    "_css_module_plugin": attr.label(
        default = Label("//ts/private/css:css_module_vite_plugin"),
        allow_single_file = True,
    ),
    "vitest": attr.label(
        doc = "Explicit label for the vitest binary.",
        allow_single_file = True,
        executable = True,
        cfg = "exec",
    ),
    "runtime": attr.label(
        doc = "Per-target override for the JS runtime binary (e.g. a custom Node wrapper). " +
              "When set, takes priority over the js_runtime toolchain.",
        allow_single_file = True,
        executable = True,
        cfg = "exec",
    ),
    "env": attr.string_dict(
        doc = "Additional environment variables for the test.",
    ),
    "environment": attr.string(
        doc = "Vitest test environment, e.g. 'node', 'jsdom', 'happy-dom', " +
              "'edge-runtime', or the name of a custom vitest environment " +
              "package.  Any value vitest accepts is allowed; the matching " +
              "package (jsdom, happy-dom, ...) must be in the target's deps.  " +
              "Emitted as test.environment in the generated config, where it " +
              "overrides an environment set by the `config` attr.",
        default = "",
    ),
    "coverage": attr.bool(
        doc = "When True, also enables vitest coverage instrumentation during " +
              "normal `bazel test` runs (in addition to `bazel coverage`).  " +
              "Coverage during `bazel coverage` is always enabled regardless " +
              "of this attr — `bazel coverage //path:test` works on every " +
              "ts_test target without any opt-in.  " +
              "Requires the @vitest/coverage-* package matching " +
              "`coverage_provider` to be present in node_modules.",
        default = False,
    ),
    "config": attr.label(
        doc = "A vitest config file (.ts/.mts/.js/.mjs).  It is MERGED into the " +
              "generated config rather than replacing it, so the Bazel-owned " +
              "machinery (CSS-module plugin, module resolution) survives.  A " +
              "config that default-exports an array is read as a list of " +
              "vitest projects (test.projects).  Files it imports relatively " +
              "must be listed in `data`.",
        allow_single_file = [".ts", ".mts", ".cts", ".js", ".mjs", ".cjs"],
    ),
    "config_json": attr.string(
        doc = "Inline vitest config as a JSON object, occupying the same " +
              "precedence layer as the `config` file.  Set through the ts_test " +
              "macro by passing a dict to `config`.",
        default = "",
    ),
    "setup_files": attr.label_list(
        doc = "Files run before each test file (vitest test.setupFiles).  " +
              "TypeScript sources are compiled by the ts_test macro; the rule " +
              "itself takes the compiled .js.  Appended after any setupFiles " +
              "from the `config` attr.",
        allow_files = True,
    ),
    "global_setup": attr.label_list(
        doc = "Files run once for the whole test run (vitest test.globalSetup).  " +
              "TypeScript sources are compiled by the ts_test macro.",
        allow_files = True,
    ),
    "data": attr.label_list(
        doc = "Extra runfiles for the test: fixtures, files imported by a " +
              "`config` or `setup_files` entry, anything read at runtime.",
        allow_files = True,
    ),
    "srcs": attr.label_list(
        doc = "The .ts/.tsx test sources `compiled_tests` was built from. The " +
              "runner only reads their paths, to map each compiled test file " +
              "back to the snapshot file its source implies.",
        allow_files = [".ts", ".tsx", ".mts", ".cts"],
    ),
    "snapshots": attr.label_list(
        doc = "Checked-in vitest snapshot files (__snapshots__/*.snap). Listing " +
              "them makes them readable inside the test sandbox, which is what " +
              "turns a stale snapshot into a failure.",
        allow_files = [".snap"],
    ),
    "globals": attr.bool(
        doc = "Enables vitest's global describe/it/expect (test.globals). The " +
              "matching `types` entry is the ts_test macro's half; this attr " +
              "is only the runtime one.",
        default = False,
    ),
    "reporters": attr.string_list(
        doc = "Vitest reporters (test.reporters), e.g. [\"default\", \"junit\"].",
    ),
    "coverage_thresholds": attr.string_dict(
        doc = "Coverage thresholds (test.coverage.thresholds), e.g. " +
              "{\"lines\": \"80\", \"perFile\": \"true\"}.  Values that look " +
              "like numbers or booleans are emitted as such.  Only enforced when " +
              "coverage runs: `bazel coverage`, or `bazel test` with " +
              "coverage = True.",
    ),
    "coverage_provider": attr.string(
        doc = "Vitest coverage provider (test.coverage.provider): \"v8\" (vitest's " +
              "own default) or \"istanbul\".  The matching @vitest/coverage-* " +
              "package must be in the target's deps.  A test whose pool runs in " +
              "a second runtime needs \"istanbul\", which instruments at " +
              "transform time; v8 reads counters out of node's inspector, which " +
              "such a runtime does not have.",
        default = "",
        values = ["", "v8", "istanbul"],
    ),
    "update_snapshots": attr.bool(
        doc = "Internal: when True this runner writes snapshots (passes --update). " +
              "Used by the update_snapshots variant of ts_test.",
        default = False,
    ),
}

_ts_test_runner_test = rule(
    implementation = _ts_test_runner_impl,
    test = True,
    attrs = dict(
        _RUNNER_ATTRS | LAUNCHER_ATTRS,
        # lcov_merger: required by Bazel's coverage protocol.
        # When `bazel coverage` is run, Bazel invokes the lcov_merger binary to
        # merge individual coverage files from each shard into a single combined
        # report.  The `output_generator` configuration field resolves to
        # `@bazel_tools//tools/test:lcov_merger` by default (or whatever the
        # user overrides with --coverage_output_generator).
        _lcov_merger = attr.label(
            cfg = "exec",
            default = configuration_field(fragment = "coverage", name = "output_generator"),
            executable = True,
        ),
    ),
    fragments = ["coverage"],
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal test runner rule; use ts_test macro instead.",
)

# Executable (non-test) variant used when update_snapshots = True.
# `bazel run //path:update_snapshots` writes snapshot files back to the source tree.
_ts_snapshot_updater = rule(
    implementation = _ts_test_runner_impl,
    executable = True,
    attrs = _RUNNER_ATTRS | LAUNCHER_ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal snapshot-updater rule; use ts_test(update_snapshots=True) macro instead.",
)

def _compile_setup_sources(name, sources, deps, target, jsx_mode, visibility, tags):
    """Compiles the .ts/.tsx entries of `sources`, passing the rest through."""
    ts_sources = [s for s in sources if s.endswith(".ts") or s.endswith(".tsx")]
    if not ts_sources:
        return sources
    ts_compile(
        name = name,
        srcs = ts_sources,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        visibility = visibility,
        tags = tags,
    )
    return [":" + name] + [s for s in sources if s not in ts_sources]

_VITEST_GLOBALS_TYPES_ENTRY = "vitest/globals"

# The generated test compile is the ts_compile RULE, which takes one JSON blob
# rather than the macro's lib / types / compiler_options -- so the macro's
# folding of those three has to happen here too. Empty stays empty: the rule
# treats an absent value differently from an empty object. `globals` folds in
# last, after anything the target wrote.
def test_compiler_options_json(lib, types, compiler_options, globals = False):
    opts = {}
    if lib != None:
        opts["lib"] = lib
    if types != None:
        opts["types"] = types
    for key, value in (compiler_options or {}).items():
        opts[key] = value
    if globals:
        entries = opts.get("types", [])
        if _VITEST_GLOBALS_TYPES_ENTRY not in [e.strip() for e in entries]:
            opts["types"] = entries + [_VITEST_GLOBALS_TYPES_ENTRY]
    if not opts:
        return ""
    return json.encode(opts)

# ─── Public macro ─────────────────────────────────────────────────────────────

def ts_test(
        name,
        srcs,
        deps = [],
        node_modules = None,
        npm_workspace_name = "npm",
        vitest = None,
        runtime = None,
        env = {},
        size = "medium",
        timeout = None,
        tags = [],
        target = "es2022",
        jsx_mode = "react-jsx",
        declarations = "tsgo",
        lib = None,
        types = None,
        compiler_options = None,
        tsconfig = None,
        path_aliases = None,
        path_alias_srcs = None,
        visibility = None,
        environment = "",
        coverage = False,
        config = None,
        setup_files = [],
        global_setup = [],
        data = [],
        globals = False,
        reporters = [],
        coverage_thresholds = {},
        coverage_provider = "",
        snapshots = [],
        update_snapshots = False):
    """Compiles TypeScript test files and runs them with vitest.

    Internally creates a ts_compile target for the test sources, then a
    test runner rule that invokes vitest on the compiled .js outputs.

    Args:
        name:              Name of the test target.
        srcs:              TypeScript test source files (.ts, .tsx).
        deps:              ts_compile or ts_npm_package targets the tests import.
                           Include @npm// labels here; ts_test automatically builds
                           a node_modules directory tree from all deps that provide
                           NpmPackageInfo (including their transitive npm deps).

                           IMPORTANT: list ALL npm packages needed at runtime —
                           both those imported by the test files and those imported
                           by the production code under test.  Deps that are
                           ts_compile targets (non-npm) are passed through to the
                           runner unchanged; they do NOT automatically propagate
                           their own npm dependencies into the node_modules tree.
                           This mirrors how go_test works: all direct runtime
                           dependencies must be listed explicitly.

                           Gazelle handles this automatically when you run
                           `bazel run //:gazelle` — it collects imports from both
                           test files and production source files in the package.
        node_modules:      Optional: explicit node_modules target for runtime npm
                           resolution. When set, the auto-generation of an internal
                           node_modules target is skipped entirely.
        npm_workspace_name: Name of the npm workspace used by npm_translate_lock.
                           Defaults to "npm" (the conventional name for the @npm
                           repository).  Set this if your WORKSPACE uses a
                           non-default name, e.g. npm_workspace_name = "my_npm".
                           This param is informational only; it does not affect
                           rule generation when node_modules = None, because
                           auto node_modules construction uses NpmPackageInfo
                           provider detection rather than label string matching.
        vitest:            Explicit label for the vitest binary (optional).
        runtime:           Per-target JS runtime binary override (optional). Takes
                           priority over the js_runtime toolchain.
        env:               Extra environment variables for the test runner.
        size:              Bazel test size (default "medium").
        timeout:           Bazel test timeout.
        tags:              Bazel tags. `manual` also reaches the targets this
                           macro generates, which no BUILD file names and a
                           wildcard would otherwise analyse; every other tag
                           goes to the test rule alone.
        target:            ECMAScript target for the internal ts_compile.
        jsx_mode:          JSX transform mode for the internal ts_compile.
        declarations:      Declaration emitter for the internal ts_compile
                           target, "tsgo" (default) or "oxc". Nothing consumes a
                           test target's .d.ts, so there is rarely a reason to
                           move tests to "oxc" and pay for annotations on test
                           helpers.
        visibility:        Bazel visibility for the test target, and for the
                           ts_compile targets this macro generates from `srcs`,
                           `setup_files` and `global_setup`. Those default to
                           `//visibility:public` when the test declares none,
                           so that an IDE tsconfig can name them.
        environment:       Vitest test environment: 'node', 'happy-dom', or 'jsdom'.
                           Requires the corresponding package in node_modules.
        coverage:          When True, also enables coverage during `bazel test`
                           (not just `bazel coverage`).  Coverage during
                           `bazel coverage` is always on regardless of this
                           attr — every ts_test supports `bazel coverage`
                           without any opt-in.
        lib:               Forwarded to the generated ts_compile: the `lib` set the
                           tests type-check against. A worker test is what needs
                           it -- webworker is in no set `target` implies.
        types:             Forwarded to the generated ts_compile: ambient type
                           packages to put in the program. A vitest pool that
                           declares its own module (`cloudflare:test`) is
                           reachable no other way, nothing importing the
                           declaration. An entry naming a package this test's
                           `deps` do not publish is an analysis error, the same
                           as on ts_compile.
        compiler_options:  Forwarded to the generated ts_compile, for whatever
                           the two above do not cover.
        tsconfig:          Forwarded to the generated ts_compile: the package's
                           own tsconfig.json, or a ts_config target when that
                           file extends others. The three above override it.
        path_aliases:      Forwarded to the generated ts_compile: the source-level
                           alias prefixes the test files import through. A package
                           whose ts_compile needs one needs it here too -- the
                           test files are a program of their own, and `paths` is
                           one key the `tsconfig` layer cannot contribute to.
        path_alias_srcs:   Forwarded to the generated ts_compile: the files an
                           alias resolves to. A test target's srcs are the test
                           files, so an alias into the code under test is covered
                           by nothing else and fails analysis without this.
        config:            Vitest config, either a label pointing at a config file
                           (.ts/.mts/.js/.mjs) or an inline dict.  It is MERGED
                           into the config rules_typescript generates rather than
                           replacing it — see "Vitest config generation" in this
                           file for the layering.  A config file that
                           default-exports an array is read as a list of vitest
                           projects (test.projects), and each project in it
                           receives the Bazel layer and the attribute layer too.
                           Files the config imports relatively belong in `data`.
        setup_files:       Files run before every test file (test.setupFiles), in
                           the order listed — compiled .ts/.tsx entries first,
                           then plain .js/.mjs ones.  TypeScript entries are
                           compiled with the same deps as the tests.  All of them
                           run after any setupFiles the `config` attr contributes.
        global_setup:      Files run once around the whole test run
                           (test.globalSetup); compiled like setup_files.
        data:              Extra runfiles: fixtures the tests read, and files that
                           `config` or `setup_files` entries import.
        globals:           Enables vitest's global describe/it/expect
                           (test.globals), and adds "vitest/globals" to the
                           compile's `types` so the type program sees them too.
                           Requires vitest among `deps`, which is where a
                           `types` entry is resolved from.
        reporters:         Vitest reporters (test.reporters).
        coverage_thresholds: Coverage thresholds (test.coverage.thresholds), e.g.
                           {"lines": "80"}.  Enforced only when coverage runs
                           (`bazel coverage`, or `bazel test` with
                           coverage = True).
        coverage_provider: Vitest coverage provider, "v8" (vitest's default) or
                           "istanbul".  A pool that runs the tests in a second
                           runtime needs "istanbul": v8 coverage is read out of
                           node's inspector, which that runtime does not have.
                           The matching @vitest/coverage-* package must be in
                           deps.
        snapshots:         Checked-in vitest snapshot files
                           (`glob(["__snapshots__/*.snap"])`). Listing them is
                           what makes them readable inside the test sandbox,
                           and so what turns a stale one into a failure; a
                           snapshot the test needs and cannot read fails too.
                           Write them with the generated updater:

                               bazel run //path:my_test.update_snapshots

        update_snapshots:  Makes THIS target the executable updater instead of
                           a test. Every ts_test already declares
                           `<name>.update_snapshots`, so this is only for an
                           updater that has to stand on its own — and it
                           compiles `srcs` itself, so it cannot share a package
                           with a ts_test over the same files.

    Example:
        ts_test(
            name = "button_test",
            srcs = ["Button.test.tsx"],
            deps = [":button", "@npm//:react", "@npm//:vitest"],
        )

    DOM testing example:
        ts_test(
            name = "component_test",
            srcs = ["Button.test.tsx"],
            deps = [":button", "@npm//:react", "@npm//:@testing-library/react", "@npm//:vitest"],
            environment = "happy-dom",
        )

    Custom npm workspace example:
        ts_test(
            name = "schema_test",
            srcs = ["schema.test.ts"],
            deps = [":schema", "@my_npm//:zod", "@my_npm//:vitest"],
            npm_workspace_name = "my_npm",
        )
    """

    fail_on_mixed_src_packages("ts_test", name, srcs, declarations, True)

    # Step 1: compile the test source files. Their declarations are the only
    # handle on the test's own types -- an IDE tsconfig has to be able to name
    # this target -- and `//visibility:private` here is one no BUILD file can
    # widen, so the test's own visibility decides, public when it has none.
    compile_name = "_{}_compile".format(name)
    compile_visibility = visibility if visibility else ["//visibility:public"]

    # `manual` is the tag a wildcard reads, and the targets below it are named
    # in no BUILD file, so a `bazel build //...` that skipped the test would
    # analyse them anyway -- which is not skipping the test:
    # //tests/compiler_options/analysis has a manual ts_test whose generated
    # compile is asserted to fail at analysis. So `manual` reaches every target
    # this macro generates. Every other tag says how the test runs, which is
    # nothing to a compile or a `bazel run`.
    wildcard_tags = ["manual"] if "manual" in tags else []

    ts_compile(
        name = compile_name,
        srcs = srcs,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        declarations = declarations,
        compiler_options_json = test_compiler_options_json(lib, types, compiler_options, globals),
        tsconfig = tsconfig,
        path_aliases = path_aliases,
        path_alias_srcs = path_alias_srcs,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )

    # Step 2: auto-generate a node_modules target when not explicitly provided.
    #
    # The _ts_auto_node_modules rule accepts any deps (no provider constraint)
    # and filters to those that provide NpmPackageInfo at analysis time.  This
    # means ALL deps — both @npm// labels and ts_compile targets — are passed
    # through.  The rule silently skips deps that don't provide NpmPackageInfo.
    #
    # If deps is a select() expression we cannot iterate over it at macro
    # evaluation time, so we skip auto-generation and require an explicit
    # node_modules attr in that case.
    if node_modules == None:
        if type(deps) != "list":
            # deps is a select() or other non-list expression; skip auto-generation.
            # The caller must set node_modules explicitly when using select() in deps.
            pass
        elif deps:
            nm_name = "_{}_node_modules".format(name)
            _ts_auto_node_modules(
                name = nm_name,
                deps = deps,
                visibility = ["//visibility:private"],
                tags = wildcard_tags,
            )
            node_modules = ":{}".format(nm_name)

    # Step 3: compile the TypeScript setup files.  vitest loads setupFiles and
    # globalSetup through the same module graph as the tests, so they have to be
    # JavaScript by the time the runner starts.
    setup_labels = _compile_setup_sources(
        name = "_{}_setup".format(name),
        sources = setup_files,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )
    global_setup_labels = _compile_setup_sources(
        name = "_{}_global_setup".format(name),
        sources = global_setup,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )

    # Step 4: assemble the runner rule kwargs.
    runner_kwargs = {
        "name": name,
        "compiled_tests": [":{}".format(compile_name)],
        "srcs": srcs,
        "snapshots": snapshots,
        "deps": deps,
        "env": env,
        "environment": environment,
        "coverage": coverage,
        "coverage_thresholds": coverage_thresholds,
        "coverage_provider": coverage_provider,
        "data": data,
        "global_setup": global_setup_labels,
        "globals": globals,
        "reporters": reporters,
        "setup_files": setup_labels,
        "update_snapshots": update_snapshots,
    }
    if node_modules:
        runner_kwargs["node_modules"] = node_modules
    if vitest:
        runner_kwargs["vitest"] = vitest
    if runtime:
        runner_kwargs["runtime"] = runtime
    if visibility:
        runner_kwargs["visibility"] = visibility
    if type(config) == "dict":
        runner_kwargs["config_json"] = json.encode(config)
    elif config:
        runner_kwargs["config"] = config

    if update_snapshots:
        # Produce an executable target (not a test) so `bazel run` works.
        # size/timeout are test-only attrs; omit them for the executable rule.
        _ts_snapshot_updater(**(runner_kwargs | {"tags": wildcard_tags}))
        return

    # The updater has to share the test's compiled sources: a second ts_compile
    # over the same srcs in the same package declares the same .js outputs.
    _ts_snapshot_updater(
        **(runner_kwargs | {
            "name": "{}.update_snapshots".format(name),
            "update_snapshots": True,
            "tags": wildcard_tags,
        })
    )

    runner_kwargs["size"] = size
    runner_kwargs["tags"] = tags
    if timeout:
        runner_kwargs["timeout"] = timeout
    _ts_test_runner_test(**runner_kwargs)
