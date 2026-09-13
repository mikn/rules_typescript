"""The generated vitest config: the entry file the launcher hands vitest.

Layers, lowest precedence first: Bazel, the user's `config`,
`coverage_provider`, the snapshot layer; docs/rules/ts-test.md § The Generated
vitest Config.
"""

load("//tools/launcher:launcher.bzl", "rlocation_path")
load("//ts/private:providers.bzl", "TsConfigInfo")
load("//ts/private:toolchain.bzl", "get_tools_toolchain")

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
const compiledForms = (spec) => {
  const m = typeof spec === 'string' ? /\\.[cm]?tsx?$/.exec(spec) : null;
  if (!m || !(m[0] in COMPILED_EXT)) return [];
  return COMPILED_EXT[m[0]].map((ext) => spec.slice(0, -m[0].length) + ext);
};
const compiledSibling = (dir, spec) =>
  compiledForms(spec).find((c) => existsSync(resolve(dir, c))) ?? spec;
// Relative or bare: a `.ts` subpath into a workspace member names a source
// the member's view holds as its compiled file.
const compiledImports = {
  name: 'rules_typescript:compiled-imports',
  enforce: 'pre',
  async resolveId(id, importer, opts) {
    if (!importer || /^[/\\0]/.test(id)) return null;
    for (const form of compiledForms(id)) {
      const resolved = await this.resolve(
        form, importer, { ...opts, skipSelf: true },
      );
      if (resolved) return resolved;
    }
    return null;
  },
};
// The run is the compiled tests in the root the launcher staged; a config's
// `include` and `dir`, written for the sources in the runfiles, are not read.
const FILES_ROOT = process.env.TS_TEST_FILES_ROOT;
if (!FILES_ROOT) {
  throw new Error('rules_typescript: TS_TEST_FILES_ROOT is unset; the ' +
    'generated config runs under the ts_test launcher');
}
const withCompiledRun = (config) => {
  const root = resolve(config.root ?? '.');
  const rewrite = (entry) => compiledSibling(root, entry);
  const test = {
    ...config.test,
    include: INCLUDE,
    dir: resolve(FILES_ROOT, relative(RUNFILES_ROOT, root)),
  };
  for (const key of ['setupFiles', 'globalSetup']) {
    if (Array.isArray(test[key])) test[key] = test[key].map(rewrite);
    else if (typeof test[key] === 'string') test[key] = rewrite(test[key]);
  }
  return { ...config, test };
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

_IDS_HELPERS = """\
// A module's id is its runfiles path where the runfiles hold the file and its
// realpath otherwise: a package's imports resolve from its place in the tree.
const SOURCE_ROOT = (() => {
  if (!SOURCE_PROBE) return null;
  try {
    const real = realpathSync(resolve(WORKSPACE_DIR, SOURCE_PROBE));
    if (real.endsWith('/' + SOURCE_PROBE)) {
      return real.slice(0, -(SOURCE_PROBE.length + 1));
    }
  } catch {}
  return null;
})();
const BIN_DIR = (() => {
  if (!BIN_PROBE) return null;
  try {
    const real = realpathSync(resolve(WORKSPACE_DIR, BIN_PROBE));
    const m = /^(.*?\\/bazel-out\\/[^/]+\\/bin)\\//.exec(real);
    if (m) return m[1];
  } catch {}
  return null;
})();
const FS_ALLOW = BIN_DIR ? [WORKSPACE_DIR, BIN_DIR] : [WORKSPACE_DIR];
const runfilesPath = (file) => {
  const out = /^.*?\\/bazel-out\\/[^/]+\\/bin\\/(.*)$/.exec(file);
  if (out) {
    const ext = /^external\\/([^/]+)\\/(.*)$/.exec(out[1]);
    const rlocation = ext ? ext[1] + '/' + ext[2] : WORKSPACE + '/' + out[1];
    return resolve(RUNFILES_ROOT, OVERLAYS[rlocation] ?? rlocation);
  }
  if (SOURCE_ROOT && file.startsWith(SOURCE_ROOT + '/')) {
    return resolve(WORKSPACE_DIR, file.slice(SOURCE_ROOT.length + 1));
  }
  return null;
};
const moduleIds = {
  name: 'rules_typescript:module-ids',
  enforce: 'pre',
  async resolveId(id, importer, opts) {
    if (id.startsWith('\\0')) return null;
    const nested = { ...opts, skipSelf: true };
    const resolved = await this.resolve(id, importer, nested);
    if (!resolved || resolved.external) return resolved;
    const cut = resolved.id.search(/[?#]/);
    const file = cut < 0 ? resolved.id : resolved.id.slice(0, cut);
    const held = file.startsWith(RUNFILES_ROOT + '/');
    if (held || file.includes('/node_modules/')) return resolved;
    const staged = runfilesPath(file);
    if (staged === null) return resolved;
    if (!existsSync(staged)) {
      this.error(
        `rules_typescript: "${id}" resolved to ${file}, which this test's ` +
          'runfiles do not hold; a src, dep or data entry has to stage it (a ' +
          'wrangler rules module is a src of the ts_compile that imports it).',
      );
    }
    const query = cut < 0 ? '' : resolved.id.slice(cut);
    return { ...resolved, id: staged + query };
  },
};
"""

def _test_file_include(extensions):
    """vitest's default include over the compiled extensions."""
    return "**/*.{test,spec}.{" + ",".join(extensions) + "}"

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
        coverage_provider,
        snapshot_bases = {},
        snapshot_root = "",
        entry_extensions = [],
        bin_probe = "",
        tsconfig_paths_rf = None,
        root_rel = ".",
        workspace_rel = ".",
        inline_members = [],
        source_probe = "",
        overlays = {},
        workspace_name = ""):
    """Builds the entry config that layers Bazel's config under the user's."""
    path_imports = "dirname, relative, resolve"
    if snapshot_bases:
        path_imports = "basename, dirname, join, relative, resolve, sep"
    lines = [
        "// AUTO-GENERATED by rules_typescript ts_test. Do not edit.",
        "//",
        "// Layers, lowest precedence first: Bazel machinery, " +
        "the user's config,",
        "// then coverage_provider and the snapshot layer, root only.",
        "// Arrays concatenate; scalars are overridden.",
        "import { " + path_imports + " } from 'node:path';",
        "import { fileURLToPath } from 'node:url';",
        "import { existsSync, readFileSync, realpathSync } from 'node:fs';",
    ]
    if user_config_rf:
        lines.append("import userConfigExport from '{}';".format(
            _relative_import(config_rf, user_config_rf),
        ))
    lines += [
        "",
        "const HERE = dirname(fileURLToPath(import.meta.url));",
        "const abs = (p) => resolve(HERE, p);",
        "const ROOT = resolve(HERE, {});".format(_js(root_rel)),
        "const WORKSPACE_DIR = resolve(HERE, {});".format(_js(workspace_rel)),
        "const RUNFILES_ROOT = dirname(WORKSPACE_DIR);",
        "const WORKSPACE = {};".format(_js(workspace_name)),
        "const OVERLAYS = {};".format(_js(overlays)),
        "const SOURCE_PROBE = {};".format(_js(source_probe)),
        "const BIN_PROBE = {};".format(_js(bin_probe)),
        "const INCLUDE = {};".format(
            _js([_test_file_include(entry_extensions)]),
        ),
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
        _IDS_HELPERS,
    ]
    if tsconfig_paths_rf:
        lines += [
            "const TSCONFIG_PATHS = JSON.parse(readFileSync(",
            "  resolve(HERE, {}), 'utf8'));".format(
                _js(_relative_import(config_rf, tsconfig_paths_rf)),
            ),
            "const PATHS_DIR = resolve(HERE, TSCONFIG_PATHS.dir);",
            "const pathsPlugins = Object.keys(TSCONFIG_PATHS.paths).length",
            "  ? [tsconfigPaths(PATHS_DIR, TSCONFIG_PATHS.paths)]",
            "  : [];",
        ]
    else:
        lines.append("const pathsPlugins = [];")

    lines += [
        # A file under test is a build output, so its realpath lies outside the
        # vite root -- which the coverage default drops before instrumenting.
        "const bazelLayer = {",
        "  root: ROOT,",
        # Vite's cache and the pool's deps optimizer write under the root
        # otherwise, which is the runfiles tree.
        "  ...(process.env.TEST_TMPDIR ? " +
        "{ cacheDir: resolve(process.env.TEST_TMPDIR, '.vite') } : {}),",
        "  plugins: [moduleIds, compiledImports, ...pathsPlugins],",
        # A DOM environment loads through Vite's server, which serves fs.allow
        # alone: the runfiles hold every runfiles id, bazel-bin every realpath.
        "  server: { fs: { allow: FS_ALLOW } },",
        # A workspace member's .js keeps its sources' extensionless relative
        # imports, which vite resolves and node's loader rejects: vite runs it.
        "  test: {{ {} }},".format(", ".join([
            "coverage: { allowExternal: true }",
            "server: {{ deps: {{ inline: [{}] }} }}".format(
                ", ".join([_member_pattern(name) for name in inline_members]),
            ),
        ])),
        "};",
    ]

    if coverage_provider:
        lines.append(
            ("const providerLayer = {{ test: {{ coverage: {{ provider: {} " +
             "}} }} }};").format(_js(coverage_provider)),
        )
    else:
        lines.append("const providerLayer = {};")

    lines.append(_snapshot_layer(snapshot_bases, snapshot_root))
    lines += [
        "",
        "export default async (env) => {",
    ]
    if user_config_rf:
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
        "  const merged = withCompiledRun(merge(" +
        "merge(merge(bazelLayer, user), providerLayer), snapshotLayer));",
        "  // Every project gets its own Vite server, so the Bazel layer " +
        "has to be",
        "  // applied to each project too; coverage and snapshots are the " +
        "root's.",
        "  const projects = merged.test && merged.test.projects;",
        "  if (Array.isArray(projects)) {",
        "    merged.test = {",
        "      ...merged.test,",
        "      projects: projects.map((p) =>",
        "        isPlainObject(p) ? withCompiledRun(merge(bazelLayer, p))" +
        " : p,",
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
        "_{}.vitest/tsconfig_paths.json".format(ctx.label.name),
    )
    chain = [ctx.file.tsconfig]
    if TsConfigInfo in ctx.attr.tsconfig:
        chain += ctx.attr.tsconfig[TsConfigInfo].deps_tsconfigs.to_list()
    ctx.actions.run(
        inputs = chain,
        outputs = [tsconfig_paths],
        executable = get_tools_toolchain(ctx).tsaction,
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

# The source root, at run time, is where this file's realpath ends.
def _source_probe(ctx):
    for f in ctx.files.srcs:
        if f.is_source:
            return f.short_path
    return ""

def _package_path(ctx, name):
    """The runfiles path of `name` in the test's package."""
    return "/".join(
        [p for p in [ctx.workspace_name, ctx.label.package, name] if p],
    )

def vitest_config_action(
        ctx,
        test_entry_points,
        entry_extensions,
        tsconfig_paths,
        inline_members,
        overlays):
    """Writes the entry config for `ctx`'s test.

    `entry_extensions` are the extensions a compiled test file has, the
    generated `test.include`'s; `tsconfig_paths` is the file
    tsconfig_paths_action wrote or None; `overlays`
    the runfiles symlinks, runfiles path to File, whose build outputs are held
    under another name than their own. Returns struct(config, entry,
    stage, symlinks, root_rel): the generated file; the runfiles path the
    launcher writes it to; every file the launcher writes into the package as a
    regular file, its destination keyed by the runfiles path it is read from;
    the private runfiles entries of `config` and `config_srcs`; and vite's root
    relative to the package.
    """
    if ctx.attr.config_srcs and not ctx.file.config:
        fail(("ts_test {}: config_srcs names the modules `config` imports; " +
              "there is no `config`.").format(ctx.label))
    name = ctx.label.name
    vitest_config = ctx.actions.declare_file(
        "_{}.vitest/config.mjs".format(name),
    )
    entry = _package_path(ctx, "_{}_vitest.config.mjs".format(name))
    stage = {rlocation_path(ctx, vitest_config): entry}

    # At its own runfiles path a source may be the package's src, which the
    # regular file takes the place of: the launcher reads it from a private one.
    symlinks = {}
    private = "/".join(
        [p for p in [ctx.label.package, "_{}.vitest".format(name)] if p],
    )
    user_config_rf = None
    if ctx.file.config:
        for f in [ctx.file.config] + ctx.files.config_srcs:
            key = private + "/" + f.short_path
            symlinks[key] = f
            stage[ctx.workspace_name + "/" + key] = rlocation_path(ctx, f)
        user_config_rf = rlocation_path(ctx, ctx.file.config)
    paths_rf = None
    if tsconfig_paths:
        paths_rf = _package_path(ctx, "_{}_tsconfig_paths.json".format(name))
        stage[rlocation_path(ctx, tsconfig_paths)] = paths_rf

    # A `config` from an ancestor package roots vite there.
    root_rel = "."
    root_dir = ctx.label.package
    if ctx.file.config:
        root_dir = ctx.file.config.short_path.rpartition("/")[0]
        root_rel = _relative_dir(ctx.label.package, root_dir)
    bin_probe = test_entry_points[0].short_path if test_entry_points else ""

    ctx.actions.write(
        output = vitest_config,
        content = _vitest_config_content(
            config_rf = entry,
            user_config_rf = user_config_rf,
            coverage_provider = ctx.attr.coverage_provider,
            snapshot_bases = _snapshot_bases(ctx.files.srcs, test_entry_points),
            snapshot_root = ctx.workspace_name,
            entry_extensions = entry_extensions,
            bin_probe = bin_probe,
            tsconfig_paths_rf = paths_rf,
            root_rel = root_rel,
            workspace_rel = _relative_dir(ctx.label.package, ""),
            inline_members = inline_members,
            source_probe = _source_probe(ctx),
            workspace_name = ctx.workspace_name,
            overlays = {
                rlocation_path(ctx, f): ctx.workspace_name + "/" + link
                for link, f in overlays.items()
                if not f.is_source
            },
        ),
    )
    return struct(
        config = vitest_config,
        entry = entry,
        stage = stage,
        symlinks = symlinks,
        root_rel = root_rel,
    )
