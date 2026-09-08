"""Test rule (macro) that compiles TypeScript tests and runs them in Bazel's
test sandbox, under vitest or under node's own runner -- see `runner`.

NOTE — Windows compatibility:
  The node_modules tree action (_ts_auto_node_modules) runs via a cross-platform
  Node.js script and works on all platforms including Windows.

  However, the test runner itself (_ts_test_runner_impl and its two rules)
  generates a bash script and is therefore NOT compatible with Windows.  Running
  `bazel test` or `bazel run` with ts_test targets on Windows requires a bash
  environment (e.g. Git Bash, WSL) or a future replacement of the runner script
  with a platform-independent alternative (see TODO.md Sub-Project 11.1).

ts_test is a macro that:
  1. Creates an internal ts_compile target for the test source files.
  2. Creates a _ts_test_runner_test test rule that runs the compiled .js outputs
     under the runner `runner` names, vitest by default.

Design:
  - srcs: the .ts/.tsx test source files
  - deps: ts_compile targets that the tests import (production code)
  - node_modules: a node_modules target for runtime npm resolution
  - vitest: optional explicit label for the vitest bin

Who controls the test environment (vitest runner):
  The user does, through `config` (a vitest config file or an inline dict) and
  through the environment attributes (setup_files, global_setup, environment,
  globals, reporters, coverage_thresholds, data).  rules_typescript keeps only
  what Bazel must own — npm resolution inside the runfiles tree and the
  coverage output paths — and
  MERGES the user's config on top of it instead of being replaced by it.  The
  layering and its precedence are described under "Vitest config generation"
  below; the config that actually ran is available as the `vitest_config`
  output group of any ts_test on the vitest runner:

    bazel build //path:my_test --output_groups=vitest_config

The vitest runner script:
  - Changes to the runfiles directory
  - Sets NODE_PATH to point at the generated node_modules tree
  - Invokes vitest with the test .js files for the current shard
  - Exits with vitest's exit code

Test sharding: the runner distributes test files across shards using
TEST_SHARD_INDEX and TEST_TOTAL_SHARDS environment variables.

npm packages at runtime:
  By default, ts_test auto-generates a node_modules tree from the deps that
  provide NpmPackageInfo (i.e. @npm// labels), their transitive npm
  dependencies (NpmPackageInfo.transitive_deps), and the npm closure every
  other dep carries in TsDeclarationInfo.transitive_npm_packages: a ts_compile
  dep's compiled JS value-imports the packages it declared, so the tree follows
  the same closure its declarations do.  Where that closure resolves one name
  more than one way, the test's own deps name the resolution that sits flat.
  The @npm workspace name is the conventional name used by
  rules_js/npm_translate_lock for the npm registry.  If your workspace uses a
  non-default name (e.g. @my_npm), pass it via the npm_workspace_name param.

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

  Writing them: every vitest ts_test also declares an executable

    bazel run //path:my_test.update_snapshots

  which runs the same compiled tests with `vitest --update` and writes the
  files under BUILD_WORKSPACE_DIRECTORY.  It shares the test's ts_compile
  target; a second ts_test over the same srcs would collide with it on the
  compiled .js outputs.

Run-time reads:
  Every vitest ts_test also declares `bazel run //path:my_test.reads`, which
  runs the same compiled tests unsandboxed under an fs hook and prints the
  workspace files they opened outside the runfiles as the labels a `data`
  entry would take; docs/rules/ts-test.md § Finding What a Test Reads.
"""

load("//tools/launcher:launcher.bzl", "LAUNCHER_ATTRS", "declare_launcher", "rlocation_path")
load("//ts/private:node_modules.bzl", "build_node_modules_action", "collect_npm_packages")
load(
    "//ts/private:providers.bzl",
    "JsInfo",
    "NpmPackageInfo",
    "TsDeclarationInfo",
)
load("//ts/private:runtime.bzl", "JS_RUNTIME_TOOLCHAIN_TYPE", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_runtime", "get_js_tool")
load("//ts/private:ts_compile.bzl", "ts_compile")
load("//ts/private:vite_config.bzl", "stage_vite_config")

# ─── Internal auto node_modules rule ──────────────────────────────────────────
#
# No provider constraint on deps: the ts_test macro passes its whole deps list,
# @npm// labels and ts_compile targets alike.

def _ts_auto_node_modules_impl(ctx):
    direct = [dep[NpmPackageInfo] for dep in ctx.attr.deps if NpmPackageInfo in dep]

    # A dep's compiled JS value-imports the packages it declared, so the tree
    # follows the closure its declarations already carry; the test's own first.
    dep_closure = depset(
        transitive = [
            dep[TsDeclarationInfo].transitive_npm_packages
            for dep in ctx.attr.deps
            if TsDeclarationInfo in dep and NpmPackageInfo not in dep
        ],
        order = "postorder",
    )
    packages_to_link = collect_npm_packages(direct + dep_closure.to_list())

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
            doc = "Any deps: an npm package is linked, and every other dep contributes the npm closure its TsDeclarationInfo carries.",
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
    doc = "Internal rule: builds a node_modules tree from the deps' npm packages and the npm closure of every other dep.",
)

# ─── Vitest config generation ────────────────────────────────────────────────
# Layers, lowest precedence first: Bazel, `config`, attributes, snapshots.

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

_SETUP_HELPERS = """\
// A setup entry or an import names a source; the program vitest runs is the
// compiled one (docs/rules/ts-test.md § Files at Run Time).
const COMPILED_EXT = {
  '.ts': ['.js'], '.tsx': ['.js', '.jsx'], '.mts': ['.mjs'], '.cts': ['.cjs'],
};
const compiledSibling = (dir, spec) => {
  const m = typeof spec === 'string' ? /\\.[cm]?tsx?$/.exec(spec) : null;
  if (!m || !(m[0] in COMPILED_EXT)) return spec;
  const sibling = COMPILED_EXT[m[0]]
    .map((ext) => spec.slice(0, -m[0].length) + ext)
    .find((candidate) => existsSync(resolve(dir, candidate)));
  return sibling ?? spec;
};
const compiledImports = {
  name: 'rules_typescript:compiled-imports',
  enforce: 'pre',
  resolveId(id, importer, opts) {
    if (!importer || !/^\\.\\.?\\//.test(id)) return null;
    const sibling = compiledSibling(dirname(importer), id);
    if (sibling === id) return null;
    return this.resolve(sibling, importer, { ...opts, skipSelf: true });
  },
};
const withCompiledSetup = (config) => {
  if (!isPlainObject(config.test)) return config;
  const root = resolve(config.root ?? '.');
  const rewrite = (entry) => compiledSibling(root, entry);
  const test = { ...config.test };
  for (const key of ['setupFiles', 'globalSetup']) {
    if (Array.isArray(test[key])) test[key] = test[key].map(rewrite);
    else if (typeof test[key] === 'string') test[key] = rewrite(test[key]);
  }
  return { ...config, test };
};

// vitest realpaths each setupFiles entry into bazel-out; a DOM environment
// then asks Vite for a file outside the root it serves: give the staged path.
const setupFilesInRoot = (config) => {
  const root = resolve(config.root ?? '.');
  const staged = new Map();
  for (const entry of [config.test?.setupFiles].flat()) {
    if (typeof entry !== 'string') continue;
    const path = resolve(root, entry);
    try {
      const real = realpathSync(path);
      if (real !== path) staged.set(real, path);
    } catch {}
  }
  if (staged.size === 0) return config;
  const plugin = {
    name: 'rules_typescript:setup-files-in-root',
    enforce: 'pre',
    resolveId: (id) =>
      staged.get(id.startsWith('/@fs/') ? id.slice(4) : id) ?? null,
  };
  return { ...config, plugins: [...(config.plugins ?? []), plugin] };
};
"""

def _package_relative_dir(ctx, f):
    """The directory of the package's output `f`, relative to the package."""
    prefix = ctx.label.package + "/" if ctx.label.package else ""
    if not f.short_path.startswith(prefix):
        fail(("ts_test {}: the node_modules tree {} is not in the test's " +
              "package, where the config is staged beside it.").format(
            ctx.label,
            f.short_path,
        ))
    return f.short_path[len(prefix):].rpartition("/")[0]

def _relative_dir(from_dir, to_dir):
    """Relative path from one workspace directory to another, "." when equal."""
    a = [p for p in from_dir.split("/") if p]
    b = [p for p in to_dir.split("/") if p]
    shared = 0
    for i in range(min(len(a), len(b))):
        if a[i] != b[i]:
            break
        shared = i + 1
    return "/".join([".."] * (len(a) - shared) + b[shared:]) or "."

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

def _member_pattern(package_name):
    """The regex literal that matches one package's files under node_modules."""
    escaped = "".join([("\\" + c) if c in "./" else c for c in package_name.elems()])
    return "/\\/node_modules\\/" + escaped + "\\//"

def _vitest_config_content(
        config_rf,
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
        update_snapshots = False,
        workers_pool_rf = None,
        root_rel = ".",
        workspace_rel = ".",
        inline_members = []):
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
        "import { existsSync, " +
        ("readFileSync, " if snapshot_bases else "") +
        "realpathSync } from 'node:fs';",
    ]
    if workers_pool_rf:
        lines.append("import {{ workersPoolLayer }} from '{}';".format(
            _relative_import(config_rf, workers_pool_rf),
        ))
    else:
        lines.append("const workersPoolLayer = undefined;")
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

    lines += [
        _CONFIG_MERGE_HELPERS,
        _SETUP_HELPERS,
        # Every path vitest is handed is a runfiles symlink; resolving them to
        # their targets walks out of the test sandbox, which the browser-like
        # environments do and the node one does not.
        #
        # A pool resolving modules for a second runtime wants the opposite (a
        # lexical path is a second identity there); workersPoolLayer turns it off.

        # A file under test is a build output, so its realpath lies outside the
        # vite root -- which the coverage default drops before instrumenting.

        # vite's default root is the working directory, which for a test is the
        # runfiles root. A config author writes paths relative to the directory
        # their config sits in, and everything that resolves against the root --
        # the Workers pool's `wrangler.configPath` among them -- then looks a
        # whole tree too high. It is resolved from the launcher's runfiles path
        # and not this file's own dirname because import.meta.url comes back as
        # the bazel-out realpath, which no runfiles path is under.
        "let bazelLayer = {",
        "  root: resolve(process.env.TS_TEST_PACKAGE_DIR, {}),".format(_js(root_rel)),
        # Vite's cache and the pool's deps optimizer write under the root otherwise,
        # which is the runfiles tree.
        "  ...(process.env.TEST_TMPDIR ? { cacheDir: resolve(process.env.TEST_TMPDIR, '.vite') } : {}),",
        "  resolve: { preserveSymlinks: true },",
        "  plugins: [compiledImports],",
        # A workspace member's .js keeps its sources' extensionless relative
        # imports, which vite resolves and node's loader rejects: vite runs it.
        "  test: {{ coverage: {{ allowExternal: true }}, server: {{ deps: {{ inline: [{}] }} }} }},".format(
            ", ".join([_member_pattern(name) for name in inline_members]),
        ),
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
        "  if (typeof workersPoolLayer === 'function') bazelLayer = workersPoolLayer(bazelLayer, user, resolve(process.env.TS_TEST_PACKAGE_DIR, {}));".format(_js(workspace_rel)),
        "  const merged = setupFilesInRoot(withCompiledSetup(merge(merge(merge(bazelLayer, user), attrLayer), snapshotLayer)));",
        "  // Every project gets its own Vite server, so the Bazel layer and the",
        "  // attribute layer have to be applied to each project too.",
        "  const projects = merged.test && merged.test.projects;",
        "  if (Array.isArray(projects)) {",
        "    merged.test = {",
        "      ...merged.test,",
        "      projects: projects.map((p) =>",
        "        isPlainObject(p) ? setupFilesInRoot(withCompiledSetup(merge(merge(bazelLayer, p), attrLayer))) : p,",
        "      ),",
        "    };",
        "  }",
        "  return merged;",
        "};",
        "",
    ]
    return "\n".join(lines)

def _snapshot_bases(srcs, compiled):
    """Maps each compiled test file to the .snap path its .ts source implies.

    Keyed by the compiled path so the generated config can match the file
    vitest reports, whatever prefix the runfiles layout gives it.
    """
    compiled_by_stem = {}
    for f in compiled:
        compiled_by_stem[f.short_path[:-(len(f.extension) + 1)]] = f.short_path
    bases = {}
    for src in srcs:
        if src.extension not in ("ts", "tsx", "mts", "cts"):
            continue
        stem = src.short_path[:-(len(src.extension) + 1)]
        if stem not in compiled_by_stem:
            continue
        parent = stem[:stem.rfind("/")] if "/" in stem else ""
        bases[compiled_by_stem[stem]] = "{}/__snapshots__/{}".format(
            parent,
            src.basename,
        )
    return bases

# ─── Test runners ─────────────────────────────────────────────────────────────
#
# node:test is configured by CLI flags and the test file alone, so the rule
# rejects the vitest-shaped attrs rather than generating a config nothing reads.

RUNNER_VITEST = "vitest"
RUNNER_NODE_TEST = "node:test"

_RUNNERS = [RUNNER_VITEST, RUNNER_NODE_TEST]

# ─── Internal test runner rule ─────────────────────────────────────────────────

# A test inside a member resolves the member's name through the nearest
# package.json, so the manifest as built stands at the member's own path.
def _member_manifests(ctx, members):
    out = {}
    for info in members:
        member_dir = info.package_root.removeprefix(ctx.bin_dir.path + "/")
        for f in info.all_files.to_list():
            if f.basename == "package.json" and not f.is_source:
                out[member_dir + "/package.json"] = f
    return out

def _without(files, paths):
    if not paths:
        return files
    return depset([f for f in files.to_list() if f.short_path not in paths])

def _same_package(a, b):
    return a.package == b.package and a.repo_name == b.repo_name

# Another package's files reach a test through `data`.
def _package_sources(ctx):
    return [
        dep[JsInfo].source_files
        for dep in ctx.attr.deps
        if JsInfo in dep and _same_package(dep.label, ctx.label)
    ]

def _ts_test_runner_impl(ctx):
    # Collect transitive .js files from all deps.
    transitive_js_sets = []
    transitive_data_sets = []
    for dep in ctx.attr.deps:
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
            transitive_data_sets.append(dep[JsInfo].transitive_data_files)

    transitive_js = depset(transitive = transitive_js_sets, order = "postorder")

    # The test .js files come from the compiled test target.
    test_js_files = ctx.files.compiled_tests

    # DefaultInfo also carries the .d.ts and .js.map beside every module; vitest
    # takes this list as the files to run.
    test_entry_points = [
        f
        for f in test_js_files
        if f.extension in ("js", "jsx", "mjs", "cjs")
    ]

    # Collect the node_modules directory.
    node_modules_files = ctx.files.node_modules

    # The workspace members in the tree: the packages with no extracted manifest.
    npm_direct = [dep[NpmPackageInfo] for dep in ctx.attr.deps if NpmPackageInfo in dep]
    npm_closure = depset(
        transitive = [
            dep[TsDeclarationInfo].transitive_npm_packages
            for dep in ctx.attr.deps
            if TsDeclarationInfo in dep and NpmPackageInfo not in dep
        ],
        order = "postorder",
    )
    members = [
        info
        for info in collect_npm_packages(npm_direct + npm_closure.to_list())
        if info.package_dir == None
    ]
    inline_members = sorted({m.package_name: True for m in members}.keys())
    member_manifests = _member_manifests(ctx, members)
    runtime_data_sets = [_without(
        depset(transitive = transitive_data_sets),
        member_manifests,
    )]
    package_sources = _package_sources(ctx)

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

    if ctx.attr.runner == RUNNER_NODE_TEST:
        return _node_test_providers(
            ctx,
            runtime_files = depset(
                transitive = [transitive_js] + runtime_data_sets +
                             package_sources,
            ),
            test_files_list = test_files_list,
            node_modules_files = node_modules_files,
            runtime_binary = runtime_binary,
            runtime_args = runtime_args,
            symlinks = member_manifests,
        )

    # ── Vitest config ─────────────────────────────────────────────────────────
    # One generated entry config layers Bazel machinery, the `config` attr and
    # the environment-shaping attributes.  See "Vitest config generation" above.
    vitest_config = ctx.actions.declare_file(
        "_{}_vitest.config.mjs".format(ctx.label.name),
    )
    if ctx.file.config and ctx.attr.config_json:
        fail("ts_test: `config` takes either a config file or an inline dict, not both.")

    # The config's copy and the package modules it imports go beside the runtime
    # tree, the one directory whose realpath resolves both: vite_config.bzl.
    user_config = None
    staged_config_files = []
    if ctx.attr.config_srcs and not ctx.file.config:
        fail(("ts_test {}: config_srcs names the modules `config` imports; " +
              "there is no `config`.").format(ctx.label))
    if ctx.file.config:
        if not node_modules_files:
            fail(("ts_test {}: a `config` resolves its imports beside the " +
                  "node_modules tree, and this test has none; a dep on the " +
                  "vitest package brings it.").format(ctx.label))
        staged_config = stage_vite_config(
            ctx,
            ctx.file.config,
            ctx.files.config_srcs,
            _package_relative_dir(ctx, node_modules_files[0]),
        )
        user_config = staged_config.entry
        staged_config_files = staged_config.files

    # The pool's half of the Bazel layer, copied beside the generated config:
    # vitest resolves the config's imports from its real path in bin.
    workers_pool = None
    if ctx.file.config:
        workers_pool = ctx.actions.declare_file(
            "_{}_workers_pool.mjs".format(ctx.label.name),
        )
        ctx.actions.expand_template(
            template = ctx.file._workers_pool,
            output = workers_pool,
            substitutions = {},
        )

    # The pool boots the file `main` names, the source; a copy naming the compiled
    # entry takes the source's runfiles path, so `wrangler.configPath` reads it.
    wrangler_patched = None
    if ctx.file.wrangler_config:
        src = ctx.file.wrangler_config

        # A runfiles file at a symlink's path wins over it silently, and the
        # unpatched `main` with it: the source's copy in data or through a dep.
        for f in ctx.files.data:
            if f.short_path == src.short_path:
                fail("ts_test {}: {} is staged through wrangler_config; do not list it in data too.".format(ctx.label, src.short_path))
        runtime_data_sets = [depset([
            f
            for f in depset(transitive = runtime_data_sets).to_list()
            if f.short_path != src.short_path
        ])]

        js_tool = get_js_tool(ctx)
        if not js_tool:
            fail("ts_test {}: wrangler_config needs the JS tool toolchain to patch the config.".format(ctx.label))
        if not node_modules_files:
            fail("ts_test {}: wrangler_config needs a node_modules tree holding wrangler; the pool in deps brings it.".format(ctx.label))
        wrangler_patched = ctx.actions.declare_file("_{}_wrangler.{}".format(ctx.label.name, src.extension))
        ctx.actions.run(
            inputs = depset([src, ctx.file._wrangler_patch] + node_modules_files),
            outputs = [wrangler_patched],
            executable = js_tool.runtime_binary,
            arguments = js_tool.args_prefix + [
                ctx.file._wrangler_patch.path,
                "--config",
                src.path,
                "--out",
                wrangler_patched.path,
                "--node-modules",
                node_modules_files[0].path,
            ],
            mnemonic = "WranglerTestConfig",
            progress_message = "WranglerTestConfig %{label}",
        )

    # A `config` from an ancestor package roots vite there.
    root_rel = "."
    if ctx.file.config:
        config_dir = ctx.file.config.short_path.rpartition("/")[0]
        root_rel = _relative_dir(ctx.label.package, config_dir)

    compiled_extensions = ("js", "jsx", "mjs", "cjs")
    setup_js = [
        f
        for f in ctx.files.setup_files
        if f.extension in compiled_extensions
    ]
    global_setup_js = [
        f
        for f in ctx.files.global_setup
        if f.extension in compiled_extensions
    ]
    ctx.actions.write(
        output = vitest_config,
        content = _vitest_config_content(
            config_rf = rlocation_path(ctx, vitest_config),
            user_config_rf = rlocation_path(ctx, user_config) if user_config else None,
            user_config_json = ctx.attr.config_json,
            environment = ctx.attr.environment,
            setup_files_rf = [rlocation_path(ctx, f) for f in setup_js],
            global_setup_rf = [rlocation_path(ctx, f) for f in global_setup_js],
            globals_enabled = ctx.attr.globals,
            reporters = ctx.attr.reporters,
            coverage_thresholds = ctx.attr.coverage_thresholds,
            coverage_provider = ctx.attr.coverage_provider,
            snapshot_bases = _snapshot_bases(ctx.files.srcs, test_entry_points),
            snapshot_root = ctx.workspace_name,
            test_include = [
                _relative_import(
                    rlocation_path(ctx, vitest_config),
                    rlocation_path(ctx, f),
                ).removeprefix("./")
                for f in test_entry_points
            ],
            update_snapshots = ctx.attr.update_snapshots,
            workers_pool_rf = rlocation_path(ctx, workers_pool) if workers_pool else None,
            root_rel = root_rel,
            workspace_rel = _relative_dir(ctx.label.package, ""),
            inline_members = inline_members,
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
    if ctx.attr.reads_report:
        vitest_cfg["reads_hook"] = rlocation_path(ctx, ctx.file._reads_hook)

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
        ctx.files.srcs +
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
    runfiles_files.extend(staged_config_files)
    if workers_pool:
        runfiles_files.append(workers_pool)
    if wrangler_patched:
        runfiles_files.append(wrangler_patched)
    if ctx.attr.reads_report:
        runfiles_files.append(ctx.file._reads_hook)

    symlinks = dict(member_manifests)
    if wrangler_patched:
        symlinks[ctx.file.wrangler_config.short_path] = wrangler_patched
    runfiles = ctx.runfiles(
        files = runfiles_files,
        # The .js, the data srcs and the package's sources: each is in the
        # sandbox only because it is named here.
        transitive_files = depset(
            transitive = [transitive_js] + runtime_data_sets + package_sources,
        ),
        root_symlinks = launcher.root_symlinks,
        symlinks = symlinks,
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

def _node_test_providers(
        ctx,
        runtime_files,
        test_files_list,
        node_modules_files,
        runtime_binary,
        runtime_args,
        symlinks):
    """Providers for a runner = "node:test" target: no generated config at all."""
    set_attrs = [
        attr_name
        for attr_name, value in [
            ("config", ctx.attr.config or ctx.attr.config_json),
            ("coverage", ctx.attr.coverage),
            ("coverage_provider", ctx.attr.coverage_provider),
            ("coverage_thresholds", ctx.attr.coverage_thresholds),
            ("environment", ctx.attr.environment),
            ("global_setup", ctx.attr.global_setup),
            ("globals", ctx.attr.globals),
            ("reporters", ctx.attr.reporters),
            ("setup_files", ctx.attr.setup_files),
            ("snapshots", ctx.attr.snapshots),
            ("update_snapshots", ctx.attr.update_snapshots),
            ("vitest", ctx.attr.vitest),
            ("wrangler_config", ctx.attr.wrangler_config),
        ]
        if value
    ]
    if set_attrs:
        fail(('ts_test {}: runner "{}" reads none of {}. Every one of them ' +
              "configures vitest, which this target does not run. Drop them, " +
              "or drop `runner` to run the test under vitest.").format(
            ctx.label,
            RUNNER_NODE_TEST,
            ", ".join(set_attrs),
        ))

    node_test_cfg = {
        "test_files_list": rlocation_path(ctx, test_files_list),
        "resolve_hook": rlocation_path(ctx, ctx.file._node_test_hook),
    }
    if node_modules_files:
        node_test_cfg["node_modules"] = rlocation_path(ctx, node_modules_files[0])

    config = {
        "label": str(ctx.label),
        "mode": "node_test",
        "workspace": ctx.workspace_name,
        "runtime_args": runtime_args,
        "env": dict(ctx.attr.env),
        "node_test": node_test_cfg,
    }
    if runtime_binary:
        config["runtime"] = rlocation_path(ctx, runtime_binary)

    launcher = declare_launcher(ctx, config, basename = "{}_test_launcher".format(ctx.label.name))

    runfiles_files = (
        [test_files_list, ctx.file._node_test_hook] + launcher.files +
        ctx.files.compiled_tests +
        ctx.files.srcs +
        node_modules_files +
        ctx.files.data
    )
    if runtime_binary:
        runfiles_files.append(runtime_binary)

    runfiles = ctx.runfiles(
        files = runfiles_files,
        transitive_files = runtime_files,
        root_symlinks = launcher.root_symlinks,
        symlinks = symlinks,
    )
    for target in ctx.attr.data:
        runfiles = runfiles.merge(target[DefaultInfo].default_runfiles)

    return [
        DefaultInfo(
            executable = launcher.executable,
            runfiles = runfiles,
        ),
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
        allow_files = [".js", ".jsx"],
    ),
    "deps": attr.label_list(
        aspects = [_instrumented_files_aspect],
        doc = "ts_compile and other targets whose .js files may be available " +
              "at test runtime; a dep's data srcs are in the runfiles beside " +
              "its .js, and a dep in the test's package has its TypeScript " +
              "srcs there too, at their source paths.",
    ),
    "node_modules": attr.label(
        doc = "A node_modules target providing the runtime npm dependency tree.",
        allow_files = True,
    ),
    "_workers_pool": attr.label(
        default = Label("//ts/private:vitest_workers_pool.mjs"),
        allow_single_file = True,
    ),
    "_wrangler_patch": attr.label(
        default = Label("//ts/private:wrangler_test_config.mjs"),
        allow_single_file = True,
    ),
    "_node_test_hook": attr.label(
        default = Label("//ts/private:node_test_hook.mjs"),
        allow_single_file = True,
    ),
    "_reads_hook": attr.label(
        default = Label("//ts/private:reads_hook.cjs"),
        allow_single_file = True,
    ),
    "runner": attr.string(
        doc = "Which test runner runs the compiled tests: \"vitest\" (default) " +
              "or \"node:test\", node's own runner, for tests written against " +
              "the node:test module.  A vitest attr set under \"node:test\" is " +
              "an analysis error rather than a silently dropped setting.",
        default = RUNNER_VITEST,
        values = _RUNNERS,
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
              "vitest ts_test target without any opt-in.  " +
              "Requires the @vitest/coverage-* package matching " +
              "`coverage_provider` to be present in node_modules.",
        default = False,
    ),
    "config": attr.label(
        doc = "A vitest config file (.ts/.mts/.js/.mjs).  It is MERGED into the " +
              "generated config rather than replacing it, so the Bazel-owned " +
              "layer (root, cacheDir, preserveSymlinks, " +
              "coverage.allowExternal, the Workers-pool half) survives.  A " +
              "config that default-exports an array is read as a list of " +
              "vitest projects (test.projects).  The modules it imports " +
              "relatively are `config_srcs`.",
        allow_single_file = [".ts", ".mts", ".cts", ".js", ".mjs", ".cjs"],
    ),
    "config_srcs": attr.label_list(
        doc = "The modules `config` imports relatively, and theirs: staged " +
              "with the config's copy at their paths relative to the " +
              "config's package, so its imports resolve there.  A file " +
              "outside that package is an analysis error.",
        allow_files = True,
    ),
    "config_json": attr.string(
        doc = "Inline vitest config as a JSON object, occupying the same " +
              "precedence layer as the `config` file.  Set through the ts_test " +
              "macro by passing a dict to `config`.",
        default = "",
    ),
    "wrangler_config": attr.label(
        doc = "The wrangler config a Workers-pool `config` names through " +
              "`wrangler.configPath`.  A copy whose `main` (and every " +
              "`env.<name>.main`) names the compiled entry beside it is staged " +
              "at this file's own runfiles path; the file is not also listed in `data`.",
        allow_single_file = [".jsonc", ".json", ".toml"],
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
        doc = "Extra runfiles for the test: fixtures, files a `setup_files` " +
              "entry imports, anything read at runtime.",
        allow_files = True,
    ),
    "srcs": attr.label_list(
        doc = "The TypeScript test sources `compiled_tests` was built from, " +
              "staged in the runfiles at their source paths; each compiled " +
              "test file maps back to the snapshot file its source implies.",
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
    "reads_report": attr.bool(
        doc = "Internal: when True this runner preloads the fs hook and " +
              "prints the workspace files the tests read outside the " +
              "runfiles. Used by the .reads variant of ts_test.",
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
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal test runner rule; use ts_test macro instead.",
)

_ts_test_runner_binary = rule(
    implementation = _ts_test_runner_impl,
    executable = True,
    attrs = _RUNNER_ATTRS | LAUNCHER_ATTRS,
    toolchains = [
        config_common.toolchain_type(JS_RUNTIME_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = "Internal executable runner: a test's `.update_snapshots` and " +
          "`.reads` companions, and a ts_test(update_snapshots = True).",
)

def _compile_setup_sources(name, sources, deps, tsconfig, visibility, tags):
    """Compiles the .ts/.tsx entries of `sources`, passing the rest through."""
    ts_sources = [s for s in sources if s.endswith(".ts") or s.endswith(".tsx")]
    if not ts_sources:
        return sources
    ts_compile(
        name = name,
        srcs = ts_sources,
        deps = deps,
        tsconfig = tsconfig,
        visibility = visibility,
        tags = tags,
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
        args = [],
        size = "medium",
        timeout = None,
        tags = [],
        tsconfig = None,
        visibility = None,
        runner = RUNNER_VITEST,
        environment = "",
        coverage = False,
        config = None,
        config_srcs = [],
        wrangler_config = None,
        setup_files = [],
        global_setup = [],
        data = [],
        globals = False,
        reporters = [],
        coverage_thresholds = {},
        coverage_provider = "",
        snapshots = [],
        update_snapshots = False):
    """Compiles TypeScript test files and runs them under vitest or node:test.

    Internally creates a ts_compile target for the test sources, then a
    test runner rule that runs the compiled .js outputs under `runner`.

    Args:
        name:              Name of the test target.
        srcs:              TypeScript test source files (.ts, .tsx).
        deps:              ts_compile or ts_npm_package targets the tests import.
                           ts_test builds a node_modules tree from every dep that
                           provides NpmPackageInfo, their transitive npm deps, and
                           the npm closure each other dep carries in
                           TsDeclarationInfo, so a ts_compile dep's own npm
                           packages are at hand at runtime without being listed
                           here.  A package the test files themselves import is a
                           direct dep, as in any ts_compile.
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
        args:              The runner's command-line flags: node's under
                           "node:test" (`--experimental-test-module-mocks`
                           for `mock.module`), vitest's under "vitest";
                           `bazel test --test_arg` appends to them.
        size:              Bazel test size (default "medium").
        timeout:           Bazel test timeout.
        tags:              Bazel tags. `manual` also reaches the targets this
                           macro generates, which no BUILD file names and a
                           wildcard would otherwise analyse; every other tag
                           goes to the test rule alone.
        visibility:        Bazel visibility for the test target, and for the
                           ts_compile targets this macro generates from `srcs`,
                           `setup_files` and `global_setup`. Those default to
                           `//visibility:public` when the test declares none,
                           so that an IDE tsconfig can name them.
        runner:            Which runner runs the compiled tests: "vitest"
                           (default) or "node:test". "node:test" is for tests
                           written against node's own runner -- vitest's
                           collector never sees a `test()` registered with
                           node:test, so such a file collects zero tests under
                           the default. `tsconfig` applies on either runner.
                           node:test configures itself
                           from CLI flags and the test file, so every
                           vitest-shaped attr (`config`, `environment`,
                           `globals`, `reporters`, `setup_files`,
                           `global_setup`, `snapshots`, the coverage trio and
                           `vitest`) is an analysis error under it, and
                           `bazel coverage` is unsupported. `--test_filter`
                           reaches it as node's --test-name-pattern; sharding
                           works as it does for vitest.
        environment:       Vitest test environment: 'node', 'happy-dom', or 'jsdom'.
                           Requires the corresponding package in node_modules.
        coverage:          When True, also enables coverage during `bazel test`
                           (not just `bazel coverage`).  Coverage during
                           `bazel coverage` is always on regardless of this
                           attr — every vitest ts_test supports `bazel coverage`
                           without any opt-in.
        tsconfig:          Forwarded to every ts_compile this macro generates --
                           the one over `srcs` and the ones over `setup_files`
                           and `global_setup`: the package's own tsconfig.json,
                           or a ts_config target when that file extends others.
                           Every compiler option the tests check under is its:
                           the `lib` a worker test needs, a `types` entry naming
                           a pool's ambient module (`cloudflare:test`) or
                           `vitest/globals`, the `paths` the test files import
                           through.
        config:            Vitest config, either a label pointing at a config file
                           (.ts/.mts/.js/.mjs) or an inline dict.  It is MERGED
                           into the config rules_typescript generates rather than
                           replacing it — see "Vitest config generation" in this
                           file for the layering.  A config file that
                           default-exports an array is read as a list of vitest
                           projects (test.projects), and each project in it
                           receives the Bazel layer and the attribute layer too.
        config_srcs:       The modules `config` imports relatively, and theirs,
                           staged with the config's copy at their paths relative
                           to the config's package.
        wrangler_config:   The wrangler config a Workers-pool `config` names
                           through `wrangler.configPath`. A copy whose `main`
                           (and every `env.<name>.main`) names the compiled
                           entry beside it is staged at this file's own runfiles
                           path; the file is not also listed in `data`.
        setup_files:       Files run before every test file (test.setupFiles), in
                           the order listed — compiled .ts/.tsx entries first,
                           then plain .js/.mjs ones.  TypeScript entries are
                           compiled with the same deps as the tests.  All of them
                           run after any setupFiles the `config` attr contributes.
        global_setup:      Files run once around the whole test run
                           (test.globalSetup); compiled like setup_files.
        data:              Extra runfiles: fixtures the tests read, and files that
                           `setup_files` entries import.
        globals:           Enables vitest's global describe/it/expect
                           (test.globals). The type program sees them through a
                           `types` entry naming "vitest/globals" in `tsconfig`,
                           with vitest among `deps`.
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
                           a test. Every vitest ts_test already declares
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

    # Step 1: compile the test source files. Their declarations are the only
    # handle on the test's own types -- an IDE tsconfig has to be able to name
    # this target -- and `//visibility:private` here is one no BUILD file can
    # widen, so the test's own visibility decides, public when it has none.
    compile_name = "_{}_compile".format(name)
    compile_visibility = visibility if visibility else ["//visibility:public"]

    # `manual` reaches every generated target: a wildcard that skips the test
    # must not analyse its compiles; //tests/node_test/analysis fails by design.
    wildcard_tags = ["manual"] if "manual" in tags else []

    ts_compile(
        name = compile_name,
        srcs = srcs,
        deps = deps,
        tsconfig = tsconfig,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )

    # Step 2: auto-generate a node_modules target when not explicitly provided.
    #
    # _ts_auto_node_modules takes the whole deps list: @npm// labels are linked,
    # every other dep contributes its npm closure.
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
        tsconfig = tsconfig,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )
    global_setup_labels = _compile_setup_sources(
        name = "_{}_global_setup".format(name),
        sources = global_setup,
        deps = deps,
        tsconfig = tsconfig,
        visibility = compile_visibility,
        tags = wildcard_tags,
    )

    # Step 4: assemble the runner rule kwargs.
    runner_kwargs = {
        "name": name,
        "runner": runner,
        "compiled_tests": [":{}".format(compile_name)],
        "srcs": srcs,
        "snapshots": snapshots,
        "deps": deps,
        "env": env,
        "args": args,
        "environment": environment,
        "coverage": coverage,
        "coverage_thresholds": coverage_thresholds,
        "coverage_provider": coverage_provider,
        "config_srcs": config_srcs,
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
    if wrangler_config:
        runner_kwargs["wrangler_config"] = wrangler_config

    if update_snapshots:
        # Produce an executable target (not a test) so `bazel run` works.
        # size/timeout are test-only attrs; omit them for the executable rule.
        _ts_test_runner_binary(**(runner_kwargs | {"tags": wildcard_tags}))
        return

    # The companions share the test's compile (a second ts_compile over the
    # same srcs declares the same .js outputs); node:test has neither.
    if runner == RUNNER_VITEST:
        _ts_test_runner_binary(
            **(runner_kwargs | {
                "name": "{}.update_snapshots".format(name),
                "update_snapshots": True,
                "tags": wildcard_tags,
            })
        )
        _ts_test_runner_binary(
            **(runner_kwargs | {
                "name": "{}.reads".format(name),
                "reads_report": True,
                "tags": wildcard_tags,
            })
        )

    runner_kwargs["size"] = size
    runner_kwargs["tags"] = tags
    if timeout:
        runner_kwargs["timeout"] = timeout
    _ts_test_runner_test(**runner_kwargs)
