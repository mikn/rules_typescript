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
    # Force the directory to be named "node_modules" so Node.js ESM resolution
    # can find packages via its parent-directory walk algorithm.
    out_dir = build_node_modules_action(ctx, packages_to_link, input_file_sets, output_name = "node_modules")

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

# ─── CSS module vitest config generator ──────────────────────────────────────
#
# When a ts_test has deps that provide CssModuleInfo, vitest cannot import
# .module.css files at runtime in Node.js.  This internal rule generates a
# minimal vitest.config.mjs that installs a Vite plugin which transforms all
# .module.css imports into a Proxy object:
#
#   import styles from "./Button.module.css";
#   styles.button   // → "button"  (the property name)
#   styles.container // → "container"
#
# The Proxy approach avoids the need to parse the CSS at test time.  Tests can
# assert on class name strings or simply check that the import doesn't crash.

_CSS_MODULE_VITEST_CONFIG = """\
// Auto-generated vitest config for CSS module support.
// Generated by rules_typescript ts_test when CSS module deps are detected.
// The cssModulesMockPlugin transforms *.module.css imports to a Proxy that
// returns the class name string for every property lookup.
const cssModulesMockPlugin = {
  name: 'rules-ts-css-modules-mock',
  enforce: 'pre',
  resolveId(id) {
    if (id.endsWith('.module.css') || id.endsWith('.module.css?direct')) {
      return '\\0css-module:' + id;
    }
    return null;
  },
  load(id) {
    if (id.startsWith('\\0css-module:')) {
      // Return a Proxy so that any property access returns the property name.
      // This mirrors the behaviour of CSS Modules in a bundled environment
      // (each class name maps to some opaque string) but uses the key itself
      // as the value, keeping tests deterministic.
      return 'export default new Proxy({}, { get: (_, k) => typeof k === "string" ? k : undefined });';
    }
    return null;
  },
};

export default {
  plugins: [cssModulesMockPlugin],
};
"""

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

    # ── CSS module auto-config ────────────────────────────────────────────────
    # If any dep provides CssModuleInfo and no explicit config is set, generate
    # a minimal vitest.config.mjs that installs a Vite plugin to mock
    # .module.css imports.  The plugin returns a Proxy object so that any
    # property access returns the property name as a string:
    #
    #   import styles from "./Button.module.css";
    #   styles.button   // → "button"
    #   styles.container // → "container"
    auto_css_config = None
    if not ctx.file.config:
        has_css_module_dep = False
        for dep in ctx.attr.deps:
            if CssModuleInfo in dep:
                has_css_module_dep = True
                break
        if has_css_module_dep:
            auto_css_config = ctx.actions.declare_file(
                "_{}_css_module_vitest.config.mjs".format(ctx.label.name),
            )
            ctx.actions.write(output = auto_css_config, content = _CSS_MODULE_VITEST_CONFIG)

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
    vitest_extra_flags = []
    if ctx.attr.environment:
        vitest_extra_flags.append('"--environment"')
        vitest_extra_flags.append('"' + _shell_escape(ctx.attr.environment) + '"')
    # Prefer explicit config; fall back to auto-generated CSS module config.
    effective_config = ctx.file.config or auto_css_config
    if effective_config:
        config_path = _rl(effective_config.short_path)
        vitest_extra_flags.append('"--config"')
        # Use ${PWD}/ prefix to make the path absolute so it works correctly
        # whether or not --root is passed to vitest.
        vitest_extra_flags.append('"${PWD}/' + _shell_escape(config_path) + '"')
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
        "if [[ -n \"$NODE_MODULES_DIR\" && -d \"$NODE_MODULES_DIR\" ]]; then\n" +
        "  export NODE_PATH=\"${PWD}/${NODE_MODULES_DIR}:${NODE_PATH:-}\"\n" +
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
        "      ln -sf \"${PWD}/${NODE_MODULES_DIR}\" \"${_NM_SYMLINK}\" || true\n" +
        "    fi\n" +
        "  fi\n" +
        "fi\n" +
        "\n" +
        "# Resolve the JS runtime binary.\n" +
        "RUNTIME=\"" + runtime_path + "\"\n" +
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
        "done < \"" + test_files_list_path + "\"\n" +
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
        # When coverage = True and COVERAGE_OUTPUT_FILE is NOT set (bazel test),
        # pass --coverage so the user can still collect coverage manually if
        # desired but Bazel won't complain about a missing output.
        #
        # @vitest/coverage-v8 must be in the target's npm deps.
        (
            "# Coverage: collect lcov when COVERAGE_OUTPUT_FILE is set by bazel coverage.\n" +
            "if [[ -n \"${COVERAGE_OUTPUT_FILE:-}\" ]]; then\n" +
            "  COVERAGE_DIR=\"$(dirname \"${COVERAGE_OUTPUT_FILE}\")\"\n" +
            "  mkdir -p \"${COVERAGE_DIR}\"\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.enabled\" \"true\")\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.provider\" \"v8\")\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.reporter\" \"lcov\")\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.reportsDirectory\" \"${COVERAGE_DIR}\")\n" +
            # When coverage is enabled, Vite needs to find @vitest/coverage-v8 from
            # the CWD (RUNFILES_DIR root).  The node_modules directory is at
            # NODE_MODULES_DIR (e.g. _main/tests/vitest/coverage/node_modules), which
            # may be several levels below the RUNFILES root.  Vite's ESM resolver does
            # NOT use NODE_PATH; it walks the directory tree looking for node_modules.
            # In the Bazel linux-sandbox, this walk is blocked at the sandbox boundary.
            #
            # Fix: create a node_modules symlink at the RUNFILES root pointing to the
            # actual node_modules tree.  This makes @vitest/coverage-v8 visible from
            # CWD so Vite's resolver finds it immediately without walking.
            "  if [[ -n \"$NODE_MODULES_DIR\" && -d \"$NODE_MODULES_DIR\" ]]; then\n" +
            "    _ROOT_NM=\"${PWD}/node_modules\"\n" +
            "    if [[ ! -e \"${_ROOT_NM}\" ]]; then\n" +
            "      ln -sf \"${PWD}/${NODE_MODULES_DIR}\" \"${_ROOT_NM}\" || true\n" +
            "    fi\n" +
            "  fi\n" +
            "elif [[ \"${COVERAGE_ENABLED:-false}\" == \"true\" ]]; then\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.enabled\" \"true\")\n" +
            "  VITEST_EXTRA_FLAGS+=(\"--coverage.provider\" \"v8\")\n" +
            "fi\n"
            if ctx.attr.coverage else ""
        ) +
        "# Run vitest via the resolved runtime.\n" +
        "VITEST=\"" + vitest_path + "\"\n" +
        "VITEST_CMD=\"" + vitest_subcommand + "\"\n" +
        # When coverage is enabled and COVERAGE_OUTPUT_FILE is set, we cannot
        # use exec (which replaces the shell process) because we need to copy
        # the lcov.info file after vitest exits.  In all other cases we use
        # exec to avoid an extra shell wrapper process.
        (
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
            # Disable pipefail/errexit around the vitest invocation so we can
            # capture the exit code and still perform the lcov copy step.
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
            if ctx.attr.coverage else
            "if [[ -n \"$VITEST\" && -f \"$VITEST\" ]]; then\n" +
            (
                "  exec \"$VITEST\" \"$VITEST_CMD\" ${VITEST_EXTRA_FLAGS[@]+\"${VITEST_EXTRA_FLAGS[@]}\"} ${SHARD_FILES[@]+\"${SHARD_FILES[@]}\"}\n"
                if vitest_is_npm_bin else
                "  exec \"$RUNTIME\" ${RUNTIME_ARGS[@]+\"${RUNTIME_ARGS[@]}\"} \"$VITEST\" \"$VITEST_CMD\" ${VITEST_EXTRA_FLAGS[@]+\"${VITEST_EXTRA_FLAGS[@]}\"} ${SHARD_FILES[@]+\"${SHARD_FILES[@]}\"}\n"
            ) +
            "elif command -v vitest &>/dev/null; then\n" +
            "  exec vitest \"$VITEST_CMD\" ${VITEST_EXTRA_FLAGS[@]+\"${VITEST_EXTRA_FLAGS[@]}\"} ${SHARD_FILES[@]+\"${SHARD_FILES[@]}\"}\n" +
            "else\n" +
            "  echo \"ts_test: vitest not found. Set vitest attr or include it in node_modules.\" >&2\n" +
            "  exit 1\n" +
            "fi\n"
        )
    )

    ctx.actions.write(
        output = runner,
        content = runner_content,
        is_executable = True,
    )

    # Build runfiles.
    runfiles_files = [test_files_list] + test_js_files + node_modules_files
    if vitest_bin:
        runfiles_files.append(vitest_bin)
    if runtime_binary:
        runfiles_files.append(runtime_binary)
    if ctx.file.config:
        runfiles_files.append(ctx.file.config)
    if auto_css_config:
        runfiles_files.append(auto_css_config)

    runfiles = ctx.runfiles(
        files = runfiles_files,
        transitive_files = transitive_js,
    )

    return [
        DefaultInfo(
            executable = runner,
            runfiles = runfiles,
        ),
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
        doc = "Vitest test environment. One of 'node', 'happy-dom', or 'jsdom'. " +
              "Passed as --environment to vitest. Requires the corresponding " +
              "package (happy-dom or jsdom) to be in node_modules.",
        default = "",
        values = ["", "node", "happy-dom", "jsdom"],
    ),
    "coverage": attr.bool(
        doc = "When True, enables vitest coverage instrumentation.  During " +
              "`bazel coverage`, the runner writes an lcov report to " +
              "COVERAGE_OUTPUT_FILE as required by Bazel's coverage protocol.  " +
              "Requires @vitest/coverage-v8 to be present in node_modules.",
        default = False,
    ),
    "config": attr.label(
        doc = "Optional label pointing to a vitest.config.ts (or .js) file. " +
              "When set, passes --config <path> to vitest.",
        allow_single_file = True,
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
        isolated_declarations = True,
        visibility = None,
        environment = "",
        coverage = False,
        config = None,
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
        isolated_declarations: When False, disables isolated declarations mode
                           on the internal ts_compile target. Set to False when
                           test files use constructs that require full type
                           information for .d.ts emit (e.g. inferred return
                           types on exported functions). Defaults to True to
                           match the ts_compile default.
        visibility:        Bazel visibility for the test target.
        environment:       Vitest test environment: 'node', 'happy-dom', or 'jsdom'.
                           Requires the corresponding package in node_modules.
        coverage:          When True, passes --coverage to vitest.
        config:            Optional label pointing to a vitest.config.ts or .js file.
                           Passed as --config to vitest.
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
        isolated_declarations = isolated_declarations,
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

    # Step 3: assemble the runner rule kwargs.
    runner_kwargs = {
        "name": name,
        "compiled_tests": [":{}".format(compile_name)],
        "deps": deps,
        "env": env,
        "environment": environment,
        "coverage": coverage,
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
    if config:
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
