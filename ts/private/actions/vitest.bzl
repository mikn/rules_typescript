"""The generated vitest config: the entry file the launcher hands vitest.

Layers, lowest precedence first: Bazel, the `config` attr, the attributes,
the snapshot layer; docs/rules/ts-test.md § The Generated vitest Config.
"""

load("//tools/launcher:launcher.bzl", "rlocation_path")
load("//ts/private:providers.bzl", "TsConfigInfo")
load("//ts/private:vite_config.bzl", "stage_vite_config")

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
        .map((line) => [line.slice(0, line.indexOf(' ')), \
line.slice(line.indexOf(' ') + 1)]),
    );
  }
  return RUNFILES_MANIFEST.get(p) ?? abs(p);
};
"""

_CONFIG_MERGE_HELPERS = """\
const isPlainObject = (v) =>
  v !== null && typeof v === 'object' && !Array.isArray(v) && \
!(v instanceof RegExp);

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

_PATHS_HELPERS = """\
// tsc took the exact key, else the longest matching prefix, and the first of
// its values that resolved; the run does the same over the compiled tree.
const tsconfigPaths = (dir, paths) => {
  const entries = Object.entries(paths).map(([key, values]) => {
    const star = key.indexOf('*');
    const exact = star < 0;
    return {
      exact,
      prefix: exact ? key : key.slice(0, star),
      suffix: exact ? '' : key.slice(star + 1),
      values,
    };
  });
  entries.sort((a, b) =>
    Number(b.exact) - Number(a.exact) || b.prefix.length - a.prefix.length);
  const match = (id) => {
    for (const e of entries) {
      if (e.exact) {
        if (id === e.prefix) return { e, captured: '' };
        continue;
      }
      const fits = id.length >= e.prefix.length + e.suffix.length;
      if (fits && id.startsWith(e.prefix) && id.endsWith(e.suffix)) {
        const end = id.length - e.suffix.length;
        return { e, captured: id.slice(e.prefix.length, end) };
      }
    }
    return null;
  };
  return {
    name: 'rules_typescript:tsconfig-paths',
    enforce: 'pre',
    async resolveId(id, importer, opts) {
      if (/^[./\\0]/.test(id)) return null;
      const m = match(id);
      if (!m) return null;
      for (const value of m.e.values) {
        const target = resolve(dir, value.replace('*', m.captured));
        const resolved = await this.resolve(
          compiledSibling(dir, target), importer, { ...opts, skipSelf: true },
        );
        if (resolved) return resolved;
      }
      return null;
    },
  };
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

def _is_test_file(f):
    """A compiled `<stem>.{test,spec}.<ext>`: vitest's default include."""
    parts = f.basename.split(".")
    return len(parts) >= 3 and parts[-2] in ("test", "spec")

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
    """Renders a string attr value as a JS scalar when it looks like one."""
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

def _snapshot_layer(snapshot_bases, snapshot_root):
    """The root-only layer that redirects vitest's .snap paths.

    `resolveSnapshotPath` is one of vitest's non-project options, so this layer
    is merged at the root and never into a `test.projects` entry.
    """
    if not snapshot_bases:
        return "const snapshotLayer = {};"
    return "\n".join([
        "const snapshotLayer = {",
        "  test: {",
        "    resolveSnapshotPath: (testPath, ext) => {",
        "      const base = snapshotBase(testPath);",
        "      if (base === null) " +
        "return vitestDefaultSnapshotPath(testPath, ext);",
        "      return rlocation({prefix} + base + ext);".format(
            prefix = _js(snapshot_root + "/"),
        ),
        "    },",
        "  },",
        "};",
    ])

def _member_pattern(package_name):
    """The regex literal that matches one package's files under node_modules."""
    escaped = "".join([
        ("\\" + c) if c in "./" else c
        for c in package_name.elems()
    ])
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
        run_include = [],
        workers_pool_rf = None,
        tsconfig_paths_rf = None,
        root_rel = ".",
        workspace_rel = ".",
        inline_members = []):
    """Builds the entry config that layers Bazel, user and attr config."""
    path_imports = "dirname, resolve"
    if snapshot_bases:
        path_imports = "basename, dirname, join, resolve, sep"
    lines = [
        "// AUTO-GENERATED by rules_typescript ts_test. Do not edit.",
        "//",
        "// Layers, lowest precedence first: Bazel machinery, " +
        "the `config` attr,",
        "// then the ts_test attributes, then the snapshot layer.",
        "// Arrays concatenate; scalars are overridden.",
        "import { " + path_imports + " } from 'node:path';",
        "import { fileURLToPath } from 'node:url';",
        "import { existsSync, readFileSync, realpathSync } from 'node:fs';",
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
        _PATHS_HELPERS,
    ]
    if tsconfig_paths_rf:
        lines += [
            "const TSCONFIG_PATHS = JSON.parse(readFileSync(",
            "  resolve(process.env.TS_TEST_PACKAGE_DIR, {}), 'utf8'));".format(
                _js(_relative_import(config_rf, tsconfig_paths_rf)),
            ),
            "const PATHS_DIR = resolve(",
            "  process.env.TS_TEST_PACKAGE_DIR, TSCONFIG_PATHS.dir);",
            "const pathsPlugins = Object.keys(TSCONFIG_PATHS.paths).length",
            "  ? [tsconfigPaths(PATHS_DIR, TSCONFIG_PATHS.paths)]",
            "  : [];",
        ]
    else:
        lines.append("const pathsPlugins = [];")
    lines += [
        # preserveSymlinks: every path vitest is handed is a runfiles symlink,
        # and a realpath is outside the sandbox; a pool's layer turns it off.

        # A file under test is a build output, so its realpath lies outside the
        # vite root -- which the coverage default drops before instrumenting.

        # root is the config's package, via TS_TEST_PACKAGE_DIR: import.meta.url
        # is the bazel-out realpath, which no runfiles path is under.
        "let bazelLayer = {",
        "  root: resolve(process.env.TS_TEST_PACKAGE_DIR, {}),".format(
            _js(root_rel),
        ),
        # Vite's cache and the pool's deps optimizer write under the root
        # otherwise, which is the runfiles tree.
        "  ...(process.env.TEST_TMPDIR ? " +
        "{ cacheDir: resolve(process.env.TEST_TMPDIR, '.vite') } : {}),",
        "  resolve: { preserveSymlinks: true },",
        "  plugins: [compiledImports, ...pathsPlugins],",
        # A workspace member's .js keeps its sources' extensionless relative
        # imports, which vite resolves and node's loader rejects: vite runs it.
        ("  test: {{ coverage: {{ allowExternal: true }}, " +
         "server: {{ deps: {{ inline: [{}] }} }} }},").format(
            ", ".join([_member_pattern(name) for name in inline_members]),
        ),
        "};",
    ]

    if user_config_json:
        lines.append("const userConfigExport = {};".format(user_config_json))

    # The run is the rule's srcs: a config's include, written for the sources,
    # matches no compiled .js, and vitest would stop with "No test files found".
    test_overrides = []
    if run_include:
        test_overrides.append("  include: {},".format(_js(run_include)))
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
        test_overrides.append(
            "  coverage: {{ {} }},".format(", ".join(coverage_keys)),
        )

    if test_overrides:
        lines.append(
            "const attrLayer = {\n test: {\n" +
            "\n".join(test_overrides) + "\n } };",
        )
    else:
        lines.append("const attrLayer = {};")

    lines.append(_snapshot_layer(snapshot_bases, snapshot_root))
    lines += [
        "",
        "export default async (env) => {",
    ]
    if user_config_rf or user_config_json:
        lines += [
            "  let user = typeof userConfigExport === 'function'",
            "    ? await userConfigExport(env)",
            "    : await userConfigExport;",
            "  // A config file that default-exports an array is a list of " +
            "vitest",
            "  // projects; vitest 4 removed test.workspace and throws on it.",
            "  if (Array.isArray(user)) user = { test: { projects: user } };",
            "  if (!isPlainObject(user)) user = {};",
        ]
    else:
        lines.append("  const user = {};")
    lines += [
        "  if (typeof workersPoolLayer === 'function') bazelLayer = " +
        "workersPoolLayer(bazelLayer, user, " +
        "resolve(process.env.TS_TEST_PACKAGE_DIR, {}));".format(
            _js(workspace_rel),
        ),
        "  const merged = setupFilesInRoot(withCompiledSetup(" +
        "merge(merge(merge(bazelLayer, user), attrLayer), snapshotLayer)));",
        "  // Every project gets its own Vite server, so the Bazel layer " +
        "and the",
        "  // attribute layer have to be applied to each project too.",
        "  const projects = merged.test && merged.test.projects;",
        "  if (Array.isArray(projects)) {",
        "    merged.test = {",
        "      ...merged.test,",
        "      projects: projects.map((p) =>",
        "        isPlainObject(p) ? setupFilesInRoot(withCompiledSetup(" +
        "merge(merge(bazelLayer, p), attrLayer))) : p,",
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

def tsconfig_paths_action(ctx):
    """Writes the `paths` of the `tsconfig` chain for the generated config.

    Returns the JSON file, or None without a `tsconfig`.
    """
    if not ctx.file.tsconfig:
        return None
    tsconfig_paths = ctx.actions.declare_file(
        "_{}_tsconfig_paths.json".format(ctx.label.name),
    )
    chain = [ctx.file.tsconfig]
    if TsConfigInfo in ctx.attr.tsconfig:
        chain += ctx.attr.tsconfig[TsConfigInfo].deps_tsconfigs.to_list()
    ctx.actions.run(
        inputs = chain,
        outputs = [tsconfig_paths],
        executable = ctx.executable._tsaction,
        arguments = [
            "paths",
            "-tsconfig=" + ctx.file.tsconfig.path,
            "-package=" + ctx.label.package,
            "-bin_dir=" + ctx.bin_dir.path,
            "-out=" + tsconfig_paths.path,
        ],
        mnemonic = "TsTestPaths",
        progress_message = "TsTestPaths %{label}",
    )
    return tsconfig_paths

def vitest_config_action(
        ctx,
        test_entry_points,
        node_modules_files,
        pool_layer,
        tsconfig_paths,
        inline_members):
    """Writes the entry config for `ctx`'s test and stages its user `config`.

    `pool_layer` is the Workers pool's layer module or None; `tsconfig_paths`
    the file tsconfig_paths_action wrote or None. Returns struct(config,
    user_config_files): the generated file and the staged copy of the user's
    config with the modules it imports, [] without a `config`.
    """
    if ctx.file.config and ctx.attr.config_json:
        fail("ts_test: `config` takes either a config file or an inline " +
             "dict, not both.")
    vitest_config = ctx.actions.declare_file(
        "_{}_vitest.config.mjs".format(ctx.label.name),
    )

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

    # A `config` from an ancestor package roots vite there.
    root_rel = "."
    root_dir = ctx.label.package
    if ctx.file.config:
        root_dir = ctx.file.config.short_path.rpartition("/")[0]
        root_rel = _relative_dir(ctx.label.package, root_dir)
    root_marker = "/".join(
        [p for p in [ctx.workspace_name, root_dir, "_"] if p],
    )

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
    config_rf = rlocation_path(ctx, vitest_config)
    user_config_rf = rlocation_path(ctx, user_config) if user_config else None
    pool_rf = rlocation_path(ctx, pool_layer) if pool_layer else None
    paths_rf = rlocation_path(ctx, tsconfig_paths) if tsconfig_paths else None
    ctx.actions.write(
        output = vitest_config,
        content = _vitest_config_content(
            config_rf = config_rf,
            user_config_rf = user_config_rf,
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
            run_include = [
                _relative_import(
                    root_marker,
                    rlocation_path(ctx, f),
                ).removeprefix("./")
                for f in test_entry_points
                if _is_test_file(f)
            ],
            workers_pool_rf = pool_rf,
            tsconfig_paths_rf = paths_rf,
            root_rel = root_rel,
            workspace_rel = _relative_dir(ctx.label.package, ""),
            inline_members = inline_members,
        ),
    )
    return struct(
        config = vitest_config,
        user_config_files = staged_config_files,
    )
