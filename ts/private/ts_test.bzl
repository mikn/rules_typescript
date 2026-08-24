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
  what Bazel must own — the CSS-module mock for CssModuleInfo deps, npm
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
  Vitest snapshot files (.snap) must live in the source tree but Bazel's
  sandbox is read-only by default.  The recommended workflow is to create a
  separate executable ts_snapshot target using update_snapshots = True:

    ts_test(
        name = "my_test",
        ...
    )

    ts_test(
        name = "update_snapshots",
        srcs = [...],  # same srcs as my_test
        deps = [...],
        update_snapshots = True,  # produces an executable, not a test
    )

  Then run:

    bazel run //path:update_snapshots

  vitest writes the snapshot files back into the source tree via
  --reporter=verbose --update.  The snapshot directory must be writable; when
  running with `bazel run` the current working directory is the workspace root,
  so vitest resolves snapshot paths relative to the source files correctly.

  Alternative: use --sandbox_writable_path to make a specific directory
  writable inside the test sandbox:

    bazel test //path:my_test \\
      --sandbox_writable_path=$(pwd)/src/components/__snapshots__
"""

load("//ts/private:node_modules.bzl", "build_node_modules_action")
load("//ts/private:providers.bzl", "CssModuleInfo", "JsInfo", "NpmPackageInfo")
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "get_js_runtime")
load("//ts/private:ts_compile.bzl", "ts_compile")

# ─── Internal auto node_modules rule ──────────────────────────────────────────
#
# This rule accepts any deps (no provider constraint) and builds a node_modules
# tree from those deps that provide NpmPackageInfo.  It is used by the ts_test
# macro to handle the case where the caller passes both @npm// labels AND
# ts_compile targets in deps — the rule silently skips non-npm deps.

def _ts_auto_node_modules_impl(ctx):
    # Filter to only deps that provide NpmPackageInfo.
    npm_deps = [dep for dep in ctx.attr.deps if NpmPackageInfo in dep]

    # Collect packages_to_link and input_file_sets, deduplicating by
    # package_name@version.
    seen = {}
    packages_to_link = []

    for dep in npm_deps:
        npm_info = dep[NpmPackageInfo]
        key = "{}@{}".format(npm_info.package_name, npm_info.package_version)
        if key not in seen:
            seen[key] = True
            packages_to_link.append(npm_info)
        for dep_info in npm_info.transitive_deps.to_list():
            dep_key = "{}@{}".format(dep_info.package_name, dep_info.package_version)
            if dep_key not in seen:
                seen[dep_key] = True
                packages_to_link.append(dep_info)

    input_file_sets = [npm_info.all_files for npm_info in packages_to_link]

    # Delegate to the shared cross-platform action helper from node_modules.bzl.
    # When the JS runtime toolchain is available (which it always is here,
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
        # mandatory = True: _ts_auto_node_modules is only created inside the ts_test
        # macro, which always requires a Node.js runtime.  Requiring the toolchain
        # prevents silent fallback to the bash path on misconfigured setups.
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = True),
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
#   1. the Bazel layer   — machinery rules_typescript owns (the CSS-module mock
#                          plugin when a dep provides CssModuleInfo)
#   2. the user layer    — the `config` attr: either a vitest config file
#                          (.ts/.mts/.js/.mjs) or an inline dict
#   3. the attribute layer — `environment`, `setup_files`, `global_setup`,
#                          `globals`, `reporters`, `coverage_thresholds`
#
# Objects are merged key by key; arrays are concatenated (base first), which
# matches vite's own mergeConfig, so a user `plugins` list never displaces the
# CSS-module mock and a user `setupFiles` list never displaces `setup_files`.
# Scalars from a later layer win.
#
# Bazel's coverage output wiring stays on the vitest command line, where it
# outranks every layer: `bazel coverage` must write lcov where Bazel expects it.

_CSS_MODULE_MOCK_PLUGIN = """\
// Transforms *.module.css imports into a Proxy whose every property lookup
// returns the property name, so class names stay deterministic without a CSS
// parse at test time.
//
// The resolved id must not itself look like a CSS request: vite's own css
// plugins key off the .css suffix and would transform the module we return,
// putting their hashed class names back in place of the mock.
const cssModulesMockPlugin = {
  name: 'rules-ts-css-modules-mock',
  enforce: 'pre',
  resolveId(id) {
    const path = id.split('?')[0];
    if (path.endsWith('.module.css')) {
      return '\\0css-module:' + path + '.mock.mjs';
    }
    return null;
  },
  load(id) {
    if (id.startsWith('\\0css-module:')) {
      return 'export default new Proxy({}, { get: (_, k) => typeof k === "string" ? k : undefined });';
    }
    return null;
  },
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

def _vitest_config_content(
        config_rf,
        css_module_mock,
        user_config_rf,
        user_config_json,
        environment,
        setup_files_rf,
        global_setup_rf,
        globals_enabled,
        reporters,
        coverage_thresholds):
    """Builds the entry vitest config that layers Bazel, user and attr config."""
    lines = [
        "// AUTO-GENERATED by rules_typescript ts_test. Do not edit.",
        "//",
        "// Layers, lowest precedence first: Bazel machinery, the `config` attr,",
        "// then the ts_test attributes. Arrays concatenate; scalars are overridden.",
        "import { dirname, resolve } from 'node:path';",
        "import { fileURLToPath } from 'node:url';",
    ]
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

    base_plugins = []
    if css_module_mock:
        lines.append(_CSS_MODULE_MOCK_PLUGIN)
        base_plugins.append("cssModulesMockPlugin")

    lines.append("const bazelLayer = {{ plugins: [{}] }};".format(", ".join(base_plugins)))

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
    if coverage_thresholds:
        thresholds = ", ".join([
            "{}: {}".format(_js(k), _js_scalar(coverage_thresholds[k]))
            for k in sorted(coverage_thresholds)
        ])
        test_overrides.append("  coverage: {{ thresholds: {{ {} }} }},".format(thresholds))

    if test_overrides:
        lines.append("const attrLayer = {\n test: {\n" + "\n".join(test_overrides) + "\n } };")
    else:
        lines.append("const attrLayer = {};")

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
            "  // A config file that default-exports an array is a vitest workspace",
            "  // definition; vitest reads those from test.workspace.",
            "  if (Array.isArray(user)) user = { test: { workspace: user } };",
            "  if (!isPlainObject(user)) user = {};",
        ]
    else:
        lines.append("  const user = {};")
    lines += [
        "  const merged = merge(merge(bazelLayer, user), attrLayer);",
        "  // Every workspace project gets its own Vite server, so the Bazel layer",
        "  // and the attribute layer have to be applied to each project too.",
        "  const projects = merged.test && merged.test.workspace;",
        "  if (Array.isArray(projects)) {",
        "    merged.test = {",
        "      ...merged.test,",
        "      workspace: projects.map((p) =>",
        "        isPlainObject(p) ? merge(merge(bazelLayer, p), attrLayer) : p,",
        "      ),",
        "    };",
        "  }",
        "  return merged;",
        "};",
        "",
    ]
    return "\n".join(lines)

def _shell_escape(s):
    """Escapes a string for safe embedding in a double-quoted shell string."""
    return s.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")

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

    # Collect the node_modules directory.
    node_modules_files = ctx.files.node_modules

    # A *.module.css anywhere in the closure means something under test imports
    # one, which Node cannot load; the generated config mocks it.  ts_compile
    # always advertises CssModuleInfo, so the depset has to be the test — the
    # provider's presence alone would install the plugin everywhere.
    needs_css_module_mock = False
    for dep in ctx.attr.deps:
        if CssModuleInfo in dep and dep[CssModuleInfo].transitive_css_files.to_list():
            needs_css_module_mock = True
            break

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

    # Helper: convert a file's short_path to its runfiles-tree-relative path.
    #
    # Bazel runfiles layout with --nolegacy_external_runfiles (bzlmod default):
    #   $RUNFILES_DIR/_main/<short_path>          for main-workspace files
    #   $RUNFILES_DIR/<repo_name>/<path>          for external-repo files
    #
    # File.short_path encoding:
    #   main-workspace:   "path/to/file"          (no prefix)
    #   external-repo:    "../repo_name/path"      (leading "../")
    #
    # Therefore:
    #   short_path starting with "../" → strip ".." → use remainder as runfiles-relative
    #   otherwise                      → prepend "_main/"
    def _rl(short_path):
        if short_path.startswith("../"):
            return short_path[3:]  # strip leading "../"
        return "_main/" + short_path

    # Write a text file listing the test .js files.
    # The runner reads this to support sharding.
    # Store runfiles-relative paths (with _main/ prefix for main-workspace files).
    test_files_list = ctx.actions.declare_file(
        "{}_test_files.txt".format(ctx.label.name),
    )
    ctx.actions.write(
        output = test_files_list,
        content = "\n".join([_rl(f.short_path) for f in test_js_files]) + "\n",
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
    user_config = None
    if ctx.file.config:
        user_config = ctx.actions.declare_file("_{}_vitest.user.config.{}".format(
            ctx.label.name,
            ctx.file.config.extension,
        ))
        ctx.actions.expand_template(
            template = ctx.file.config,
            output = user_config,
            substitutions = {},
        )
    setup_js = [f for f in ctx.files.setup_files if f.extension in ("js", "mjs", "cjs")]
    global_setup_js = [f for f in ctx.files.global_setup if f.extension in ("js", "mjs", "cjs")]
    ctx.actions.write(
        output = vitest_config,
        content = _vitest_config_content(
            config_rf = _rl(vitest_config.short_path),
            css_module_mock = needs_css_module_mock,
            user_config_rf = _rl(user_config.short_path) if user_config else None,
            user_config_json = ctx.attr.config_json,
            environment = ctx.attr.environment,
            setup_files_rf = [_rl(f.short_path) for f in setup_js],
            global_setup_rf = [_rl(f.short_path) for f in global_setup_js],
            globals_enabled = ctx.attr.globals,
            reporters = ctx.attr.reporters,
            coverage_thresholds = ctx.attr.coverage_thresholds,
        ),
    )

    # Determine paths for the runner script (all as runfiles-relative paths).
    node_modules_path = _shell_escape(_rl(node_modules_files[0].short_path)) if node_modules_files else ""
    vitest_path = _shell_escape(_rl(vitest_bin.short_path)) if vitest_bin else ""

    # Fallback: resolve vitest from node_modules if not explicit.
    # Use the canonical bin entry path (vitest.mjs) as declared in vitest's
    # package.json#bin field.  This replaces the old heuristic (dist/cli.js)
    # with the authoritative ESM bin entry extracted during npm_translate_lock.
    if not vitest_path and node_modules_path:
        vitest_path = "{}/vitest/vitest.mjs".format(node_modules_path)

    runtime_path = _shell_escape(_rl(runtime_binary.short_path)) if runtime_binary else ""
    # Build the shell snippet that prefixes runtime args (e.g. "--experimental-vm-modules").
    runtime_args_str = " ".join(["\"{}\"".format(_shell_escape(a)) for a in runtime_args])

    test_files_list_path = _shell_escape(_rl(test_files_list.short_path))

    # Environment variable export lines from the env attribute.
    # Shell-escape values to prevent injection via $, `, ", \.
    env_lines = []
    for k, v in ctx.attr.env.items():
        escaped = v.replace("\\", "\\\\").replace('"', '\\"').replace("$", "\\$").replace("`", "\\`")
        env_lines.append("export {k}=\"{v}\"".format(k = k, v = escaped))
    env_setup = "\n".join(env_lines)

    # Build the vitest CLI flags beyond "run" (or "watch --update" for snapshots).
    # Each flag is emitted as a separate array element to avoid word-splitting
    # issues when paths are used as argument values.
    # `environment` and the other environment-shaping attrs live in the
    # generated config, not on the command line, so that the documented
    # precedence (attrs beat the user config) holds in one place.
    vitest_extra_flags = ['"--config"']

    # Resolved against RUNFILES_ROOT rather than PWD: update_snapshots runs from
    # the workspace root, where a runfiles-relative path does not exist.
    vitest_extra_flags.append('"${RUNFILES_ROOT}/' + _shell_escape(_rl(vitest_config.short_path)) + '"')
    if ctx.attr.update_snapshots:
        vitest_extra_flags.append('"--update"')
    vitest_flags_str = " ".join(vitest_extra_flags)

    # update_snapshots targets run vitest in the workspace root so that
    # snapshot files are written back to the source tree.
    # The vitest subcommand changes: "run --update" (write snapshots then exit).
    vitest_subcommand = "run"

    runner = ctx.actions.declare_file("{}_test_runner.sh".format(ctx.label.name))

    runner_content = (
        "#!/usr/bin/env bash\n" +
        "# Bazel-generated test runner for " + str(ctx.label) + "\n" +
        "set -euo pipefail\n" +
        "\n" +
        "# Resolve the runfiles root.\n" +
        "# Bazel sets RUNFILES_DIR; TEST_SRCDIR is the legacy name.\n" +
        "if [[ -z \"${RUNFILES_DIR:-}\" && -n \"${TEST_SRCDIR:-}\" ]]; then\n" +
        "  RUNFILES_DIR=\"$TEST_SRCDIR\"\n" +
        "fi\n" +
        "# Absolute runfiles root: every generated path below is resolved against\n" +
        "# it, so it stays correct after the cd below changes the directory.\n" +
        "RUNFILES_ROOT=\"$(cd \"${RUNFILES_DIR}\" && pwd)\"\n" +
        (
            # update_snapshots: cd to BUILD_WORKSPACE_DIRECTORY (set by `bazel run`)
            # so vitest writes .snap files back into the source tree.
            "# update_snapshots: write snapshots back into the source tree.\n" +
            "# BUILD_WORKSPACE_DIRECTORY is set by `bazel run` to the workspace root.\n" +
            "if [[ -n \"${BUILD_WORKSPACE_DIRECTORY:-}\" ]]; then\n" +
            "  cd \"${BUILD_WORKSPACE_DIRECTORY}\"\n" +
            "else\n" +
            "  cd \"${RUNFILES_DIR}\"\n" +
            "fi\n"
            if ctx.attr.update_snapshots else
            "# All paths in this script are relative to RUNFILES_DIR.\n" +
            "cd \"${RUNFILES_DIR}\"\n"
        ) +
        "\n" +
        "# Environment variables from the `env` attribute.\n" +
        env_setup + "\n" +
        "\n" +
        "# Node resolution via NODE_PATH.\n" +
        # node_modules_path is the runfiles-relative path to the node_modules tree
        # (e.g. _main/tests/vitest/node_modules).  The directory must be literally
        # named "node_modules" for Node.js ESM module resolution to work — vitest
        # uses import('@vitest/coverage-v8') which resolves via the
        # walking-parent-directories algorithm, not via NODE_PATH.
        #
        # When the explicit node_modules target is not named "node_modules" (e.g.
        # "math_coverage_node_modules"), we create a "node_modules" symlink in the
        # parent directory so that Node.js can find packages via its standard ESM
        # resolution algorithm.
        "NODE_MODULES_DIR=\"" + node_modules_path + "\"\n" +
        "if [[ -n \"$NODE_MODULES_DIR\" ]]; then\n" +
        "  NODE_MODULES_DIR=\"${RUNFILES_ROOT}/${NODE_MODULES_DIR}\"\n" +
        "fi\n" +
        "if [[ -n \"$NODE_MODULES_DIR\" && -d \"$NODE_MODULES_DIR\" ]]; then\n" +
        "  export NODE_PATH=\"${NODE_MODULES_DIR}:${NODE_PATH:-}\"\n" +
        "  # Ensure the directory is named 'node_modules' for ESM resolution.\n" +
        "  # Node.js ESM does not use NODE_PATH; it walks parent directories looking\n" +
        "  # for a directory literally named 'node_modules'.  When the target has a\n" +
        "  # different name, create a 'node_modules' symlink one level up so that\n" +
        "  # vitest running from inside the tree can locate sibling packages.\n" +
        "  _NM_BASENAME=\"$(basename \"${NODE_MODULES_DIR}\")\"\n" +
        "  if [[ \"$_NM_BASENAME\" != \"node_modules\" ]]; then\n" +
        "    _NM_PARENT_DIR=\"$(dirname \"${NODE_MODULES_DIR}\")\"\n" +
        "    _NM_SYMLINK=\"${_NM_PARENT_DIR}/node_modules\"\n" +
        "    if [[ ! -e \"${_NM_SYMLINK}\" ]]; then\n" +
        "      ln -sf \"${NODE_MODULES_DIR}\" \"${_NM_SYMLINK}\" || true\n" +
        "    fi\n" +
        "  fi\n" +
        # Vite resolves the bare imports of a vitest config file (vitest/config,
        # plugin packages) by walking up from the config's directory.  A
        # node_modules at the runfiles root terminates that walk inside the
        # sandbox for every package layout, not just the auto-generated one.
        "  _ROOT_NM=\"${RUNFILES_ROOT}/node_modules\"\n" +
        "  if [[ ! -e \"${_ROOT_NM}\" ]]; then\n" +
        "    ln -sf \"${NODE_MODULES_DIR}\" \"${_ROOT_NM}\" || true\n" +
        "  fi\n" +
        "fi\n" +
        "\n" +
        "# Resolve the JS runtime binary.\n" +
        "RUNTIME=\"" + (("${RUNFILES_ROOT}/" + runtime_path) if runtime_path else "") + "\"\n" +
        "RUNTIME_ARGS=(" + runtime_args_str + ")\n" +
        "if [[ -z \"$RUNTIME\" ]]; then\n" +
        "  # Fallback to system node.\n" +
        "  RUNTIME=\"node\"\n" +
        "fi\n" +
        "\n" +
        "# Read all test .js files (runfiles-relative paths).\n" +
        "ALL_TEST_FILES=()\n" +
        "while IFS= read -r line; do\n" +
        "  [[ -n \"$line\" ]] && ALL_TEST_FILES+=(\"$line\")\n" +
        "done < \"${RUNFILES_ROOT}/" + test_files_list_path + "\"\n" +
        "\n" +
        "# Shard support: partition files across shards.\n" +
        "SHARD_INDEX=\"${TEST_SHARD_INDEX:-0}\"\n" +
        "TOTAL_SHARDS=\"${TEST_TOTAL_SHARDS:-1}\"\n" +
        "\n" +
        "SHARD_FILES=()\n" +
        "idx=0\n" +
        "for f in \"${ALL_TEST_FILES[@]}\"; do\n" +
        "  if (( idx % TOTAL_SHARDS == SHARD_INDEX )); then\n" +
        "    SHARD_FILES+=(\"$f\")\n" +
        "  fi\n" +
        "  (( idx++ )) || true\n" +
        "done\n" +
        "\n" +
        "if [[ \"${#SHARD_FILES[@]}\" -eq 0 ]]; then\n" +
        "  echo \"ts_test: no test files assigned to shard $SHARD_INDEX/$TOTAL_SHARDS\"\n" +
        "  exit 0\n" +
        "fi\n" +
        "\n" +
        "# Extra vitest flags (environment, config) as a bash array.\n" +
        "VITEST_EXTRA_FLAGS=(" + vitest_flags_str + ")\n" +
        "\n" +
        # Coverage: when COVERAGE_OUTPUT_FILE is set (bazel coverage), configure
        # vitest to write lcov data to the directory Bazel expects, then copy
        # the lcov.info file to COVERAGE_OUTPUT_FILE after the run.
        #
        # This block is UNCONDITIONAL — `bazel coverage` works on every ts_test
        # target regardless of whether `coverage = True` is set on the target.
        # The `coverage = True` attr only affects `bazel test` (not `bazel
        # coverage`): when True it also enables coverage during normal test runs
        # via the COVERAGE_ENABLED env var.
        #
        # @vitest/coverage-v8 must be in the target's npm deps when using
        # `bazel coverage`.
        "# Coverage: collect lcov when COVERAGE_OUTPUT_FILE is set by bazel coverage.\n" +
        "# This block runs unconditionally so 'bazel coverage' works on any ts_test.\n" +
        "if [[ -n \"${COVERAGE_OUTPUT_FILE:-}\" ]]; then\n" +
        "  COVERAGE_DIR=\"$(dirname \"${COVERAGE_OUTPUT_FILE}\")\"\n" +
        "  mkdir -p \"${COVERAGE_DIR}\"\n" +
        "  VITEST_EXTRA_FLAGS+=(\"--coverage.enabled\" \"true\")\n" +
        "  VITEST_EXTRA_FLAGS+=(\"--coverage.provider\" \"v8\")\n" +
        "  VITEST_EXTRA_FLAGS+=(\"--coverage.reporter\" \"lcov\")\n" +
        "  VITEST_EXTRA_FLAGS+=(\"--coverage.reportsDirectory\" \"${COVERAGE_DIR}\")\n" +
        (
            # coverage = True: also enable coverage during `bazel test` (not just
            # `bazel coverage`) when the user explicitly opts in.
            "elif [[ \"${COVERAGE_ENABLED:-false}\" == \"true\" ]]; then\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.enabled\" \"true\")\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.provider\" \"v8\")\n"
            if ctx.attr.coverage else ""
        ) +
        "fi\n" +
        "# Run vitest via the resolved runtime.\n" +
        "VITEST=\"" + (("${RUNFILES_ROOT}/" + vitest_path) if vitest_path else "") + "\"\n" +
        "VITEST_CMD=\"" + vitest_subcommand + "\"\n" +
        # We always need the wrapper function form (not exec) so that the
        # lcov post-processing step can run after vitest exits when
        # COVERAGE_OUTPUT_FILE is set (which happens during `bazel coverage`).
        "# Coverage post-run: copy lcov.info → COVERAGE_OUTPUT_FILE if present.\n" +
        "_run_vitest() {\n" +
        "  if [[ -n \"$VITEST\" && -f \"$VITEST\" ]]; then\n" +
        (
            "    \"$VITEST\" \"$VITEST_CMD\" ${VITEST_EXTRA_FLAGS[@]+\"${VITEST_EXTRA_FLAGS[@]}\"} ${SHARD_FILES[@]+\"${SHARD_FILES[@]}\"}\n"
            if vitest_is_npm_bin else
            "    \"$RUNTIME\" ${RUNTIME_ARGS[@]+\"${RUNTIME_ARGS[@]}\"} \"$VITEST\" \"$VITEST_CMD\" ${VITEST_EXTRA_FLAGS[@]+\"${VITEST_EXTRA_FLAGS[@]}\"} ${SHARD_FILES[@]+\"${SHARD_FILES[@]}\"}\n"
        ) +
        "  elif command -v vitest &>/dev/null; then\n" +
        "    vitest \"$VITEST_CMD\" ${VITEST_EXTRA_FLAGS[@]+\"${VITEST_EXTRA_FLAGS[@]}\"} ${SHARD_FILES[@]+\"${SHARD_FILES[@]}\"}\n" +
        "  else\n" +
        "    echo \"ts_test: vitest not found. Set vitest attr or include it in node_modules.\" >&2\n" +
        "    return 1\n" +
        "  fi\n" +
        "}\n" +
        # Capture vitest's exit code so we can still perform the lcov copy
        # step even when vitest fails (Bazel expects COVERAGE_OUTPUT_FILE to
        # be written even on test failure when running under bazel coverage).
        "_exit=0\n" +
        "_run_vitest || _exit=$?\n" +
        "if [[ -n \"${COVERAGE_OUTPUT_FILE:-}\" ]]; then\n" +
        "  _lcov=\"$(dirname \"${COVERAGE_OUTPUT_FILE}\")/lcov.info\"\n" +
        "  if [[ -f \"$_lcov\" ]]; then\n" +
        # Normalise SF: paths so Bazel's _lcov_merger can match them.
        # vitest emits SF lines with the runfiles-relative path
        # (e.g. "_main/tests/vitest/math.js").  Bazel's lcov_merger
        # expects paths relative to the workspace root without the
        # "_main/" repository prefix.
        "    sed 's|^SF:_main/|SF:|' \"$_lcov\" > \"${COVERAGE_OUTPUT_FILE}\"\n" +
        "  else\n" +
        "    # Write an empty lcov file so Bazel does not fail due to a missing output.\n" +
        "    printf '' > \"${COVERAGE_OUTPUT_FILE}\"\n" +
        "  fi\n" +
        "fi\n" +
        "exit \"${_exit}\"\n"
    )

    ctx.actions.write(
        output = runner,
        content = runner_content,
        is_executable = True,
    )

    # Build runfiles.
    runfiles_files = (
        [test_files_list, vitest_config] +
        test_js_files +
        node_modules_files +
        ctx.files.setup_files +
        ctx.files.global_setup +
        ctx.files.data
    )
    if vitest_bin:
        runfiles_files.append(vitest_bin)
    if runtime_binary:
        runfiles_files.append(runtime_binary)
    if user_config:
        runfiles_files.append(user_config)

    runfiles = ctx.runfiles(
        files = runfiles_files,
        transitive_files = transitive_js,
    )
    for target in ctx.attr.data + ctx.attr.setup_files + ctx.attr.global_setup:
        runfiles = runfiles.merge(target[DefaultInfo].default_runfiles)

    return [
        DefaultInfo(
            executable = runner,
            runfiles = runfiles,
        ),
        # Exposes the config vitest actually ran with, for debugging and for
        # tests that pin the layering.
        OutputGroupInfo(vitest_config = depset([vitest_config])),
    ]

# Shared attribute dict for both the test and executable runner variants.
_RUNNER_ATTRS = {
    "compiled_tests": attr.label_list(
        doc = "Label of the ts_compile target containing compiled test .js files.",
        allow_files = [".js"],
    ),
    "deps": attr.label_list(
        doc = "ts_compile and other targets whose .js files may be available at test runtime. " +
              "Deps that do not provide JsInfo (e.g. css_module, asset_library) are silently " +
              "skipped when collecting transitive .js files.",
    ),
    "node_modules": attr.label(
        doc = "A node_modules target providing the runtime npm dependency tree.",
        allow_files = True,
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
              "Requires @vitest/coverage-v8 to be present in node_modules.",
        default = False,
    ),
    "config": attr.label(
        doc = "A vitest config file (.ts/.mts/.js/.mjs).  It is MERGED into the " +
              "generated config rather than replacing it, so the Bazel-owned " +
              "machinery (CSS-module mock, module resolution) survives.  A " +
              "config that default-exports an array is read as a vitest " +
              "workspace (test.workspace).  Files it imports relatively must be " +
              "listed in `data`.",
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
    "globals": attr.bool(
        doc = "Enables vitest's global describe/it/expect (test.globals).",
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
        _RUNNER_ATTRS,
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
    attrs = _RUNNER_ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal snapshot-updater rule; use ts_test(update_snapshots=True) macro instead.",
)

def _compile_setup_sources(name, sources, deps, target, jsx_mode):
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
        visibility = ["//visibility:private"],
    )
    return [":" + name] + [s for s in sources if s not in ts_sources]

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
        tags:              Bazel tags.
        target:            ECMAScript target for the internal ts_compile.
        jsx_mode:          JSX transform mode for the internal ts_compile.
        declarations:      Declaration emitter for the internal ts_compile
                           target, "tsgo" (default) or "oxc". Nothing consumes a
                           test target's .d.ts, so there is rarely a reason to
                           move tests to "oxc" and pay for annotations on test
                           helpers.
        visibility:        Bazel visibility for the test target.
        environment:       Vitest test environment: 'node', 'happy-dom', or 'jsdom'.
                           Requires the corresponding package in node_modules.
        coverage:          When True, also enables coverage during `bazel test`
                           (not just `bazel coverage`).  Coverage during
                           `bazel coverage` is always on regardless of this
                           attr — every ts_test supports `bazel coverage`
                           without any opt-in.
        config:            Vitest config, either a label pointing at a config file
                           (.ts/.mts/.js/.mjs) or an inline dict.  It is MERGED
                           into the config rules_typescript generates rather than
                           replacing it — see "Vitest config generation" in this
                           file for the layering.  A config file that
                           default-exports an array is read as a vitest workspace
                           (test.workspace), and each project in it receives the
                           Bazel layer and the attribute layer too.  Files the
                           config imports relatively belong in `data`.
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
                           (test.globals).
        reporters:         Vitest reporters (test.reporters).
        coverage_thresholds: Coverage thresholds (test.coverage.thresholds), e.g.
                           {"lines": "80"}.  Enforced only when coverage runs
                           (`bazel coverage`, or `bazel test` with
                           coverage = True).
        update_snapshots:  When True, creates an *executable* target (not a test)
                           that runs `vitest run --update` and writes snapshot files
                           back into the source tree. Use with `bazel run`:

                               bazel run //path:name

                           The snapshot files are written relative to
                           BUILD_WORKSPACE_DIRECTORY (the workspace root), which
                           is how vitest resolves __snapshots__ directories.

                           Typical pattern — two targets sharing the same srcs:

                               ts_test(
                                   name = "my_test",
                                   srcs = ["my.test.ts"],
                                   deps = [...],
                               )

                               ts_test(
                                   name = "update_snapshots",
                                   srcs = ["my.test.ts"],
                                   deps = [...],
                                   update_snapshots = True,
                               )

                           Alternative: pass --sandbox_writable_path to make the
                           __snapshots__ directory writable inside the test sandbox:

                               bazel test //path:my_test \\
                                 --sandbox_writable_path=\\
                                 $(pwd)/src/components/__snapshots__

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
    # Step 1: compile the test source files.
    compile_name = "_{}_compile".format(name)
    ts_compile(
        name = compile_name,
        srcs = srcs,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
        declarations = declarations,
        visibility = ["//visibility:private"],
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
    )
    global_setup_labels = _compile_setup_sources(
        name = "_{}_global_setup".format(name),
        sources = global_setup,
        deps = deps,
        target = target,
        jsx_mode = jsx_mode,
    )

    # Step 4: assemble the runner rule kwargs.
    runner_kwargs = {
        "name": name,
        "compiled_tests": [":{}".format(compile_name)],
        "deps": deps,
        "env": env,
        "environment": environment,
        "coverage": coverage,
        "coverage_thresholds": coverage_thresholds,
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
        # size/timeout/tags are test-only attrs; omit them for the executable rule.
        _ts_snapshot_updater(**runner_kwargs)
    else:
        runner_kwargs["size"] = size
        runner_kwargs["tags"] = tags
        if timeout:
            runner_kwargs["timeout"] = timeout
        _ts_test_runner_test(**runner_kwargs)
