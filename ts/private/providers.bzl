"""Provider definitions for rules_typescript.

A `direct` field carries only what the target itself produces. A rule that just
forwards a dep's files -- ts_compile relative to a dep's data files, say --
leaves the direct field empty and puts the closure in the transitive one; a
consumer that wants everything reachable reads the transitive field.
"""

TsInfo = provider(
    doc = """What a dep gives a consumer: the files its program and runtime
stage, the npm packages its closure holds and the store files they reach.

ts_compile, ts_codegen and ts_binary return it over their outputs; an npm
package target returns one naming its closure in `npm_packages` and nothing by
path, since its files reach a consumer through the importer's links into the
store; a workspace member's hub view forwards the member's.
""",
    fields = {
        "js": "depset of File: the .js this target produces -- compiled " +
              "output and JavaScript srcs staged as-is.",
        "js_maps": "depset of File: the .js.map beside them.",
        "declarations": "depset of File: the .d.ts this target produces and " +
                        "the ones it passes through from srcs. A global one " +
                        "is in scope in a consumer only when the consumer's " +
                        "tsconfig `types` names it.",
        "data": "depset of File: the other srcs, staged at their " +
                "package-relative paths beside the .js.",
        "sources": "depset of File: the TypeScript srcs -- .ts, .tsx and " +
                   "declarations. A ts_test in the same package stages them " +
                   "at their source paths.",
        "transitive_js": "depset of File: the .js of this target and its " +
                         "first-party deps.",
        "transitive_js_maps": "depset of File: their .js.map.",
        "transitive_declarations": "depset of File: the .d.ts of this " +
                                   "target and its first-party deps. An npm " +
                                   "package's reach a consumer through " +
                                   "`npm_files`, not through this depset.",
        "transitive_data": "depset of File: the data files of this target " +
                           "and its first-party deps, what a compiled module " +
                           "reaches beside itself at run time.",
        "transitive_es_twins": "depset of (File, File): for a program tsgo " +
                               "emits, each .js of this target and its " +
                               "first-party deps paired with the ES module " +
                               "oxc emits from the same source; the vitest " +
                               "runner stages the second at the first's path.",
        "npm_packages": "depset of NpmPackageInfo: the npm closure of this " +
                        "target's deps, what the ownership manifest names " +
                        "and a runner checks its packages against. A " +
                        "package itself arrives through its NpmPackageInfo.",
        "npm_files": "depset of File: the store files this target's program " +
                     "and runtime reach -- the importer links of its direct " +
                     "npm deps and their @types twins, the member links its " +
                     "deps name, every store tree and edge link of their " +
                     "closures, the hoist links whose names the closure " +
                     "holds with the trees they enter, and its first-party " +
                     "deps' npm_files. An action stages this and nothing " +
                     "else of the store.",
        "owners": "depset of struct(label, files): one record per " +
                  "first-party target in the closure, this one first -- " +
                  "the label a deps list writes and the declarations and " +
                  "data it stages. The tsgo action names the owner of a " +
                  "listed file from these.",
    },
)

_EMPTY = depset()

def label_text(label):
    """The label as a deps list writes it, the main repo's @@ dropped."""
    text = str(label)
    return text[2:] if text.startswith("@@//") else text

def _or_direct(transitive, direct):
    return direct if transitive == None else transitive

def ts_info(
        js = _EMPTY,
        js_maps = _EMPTY,
        declarations = _EMPTY,
        data = _EMPTY,
        sources = _EMPTY,
        transitive_js = None,
        transitive_js_maps = None,
        transitive_declarations = None,
        transitive_data = None,
        transitive_es_twins = _EMPTY,
        npm_packages = _EMPTY,
        npm_files = _EMPTY,
        label = None):
    """A TsInfo for a target without first-party deps: each closure it
    leaves unsaid is the direct set, and `label` makes it the one owner."""
    owners = _EMPTY
    if label:
        owners = depset([struct(
            label = label_text(label),
            files = depset(transitive = [declarations, data]),
        )])
    return TsInfo(
        js = js,
        js_maps = js_maps,
        declarations = declarations,
        data = data,
        sources = sources,
        transitive_js = _or_direct(transitive_js, js),
        transitive_js_maps = _or_direct(transitive_js_maps, js_maps),
        transitive_declarations = _or_direct(
            transitive_declarations,
            declarations,
        ),
        transitive_data = _or_direct(transitive_data, data),
        transitive_es_twins = transitive_es_twins,
        npm_packages = npm_packages,
        npm_files = npm_files,
        owners = owners,
    )

TsTestRunnerInfo = provider(
    doc = """A test runner: the target ts_test hands its compiled tests to.

//ts/runners:vitest and //ts/runners:node_test are the two shipped; a rule in
another ruleset returning this provider is a third. ts_test compiles the tests
against the importer chain they run in, and the runner's `launch` turns them
into the launcher's config and the runfiles of one test.
""",
    fields = {
        "packages": "list of string: the npm packages the runner needs in " +
                    "the test's npm closure, `vitest` for the vitest " +
                    "runner; ts_test fails at analysis naming the one no dep " +
                    "provides.",
        "hook": "File: the one module the runner loads into node before the " +
                "tests -- the node:test runner's resolver, the vitest " +
                "runner's reads recorder.",
        "es_modules": "bool: True when the runner runs the program as ES " +
                      "modules whatever its tsconfig's module -- vitest, " +
                      "which imports every file through vite's transform -- " +
                      "so ts_test emits its srcs as such and stages a dep's " +
                      "ES twins; False for node:test, which runs the " +
                      "package's format.",
        "launch": "function(ctx, test) -> struct: the runner's half of one " +
                  "test's analysis. `test` is the struct ts_test builds from " +
                  "the compile (entry_points, test_files_list, chain, " +
                  "transitive_js, es_twins, runtime_data_sets, " +
                  "package_sources, inline_members, runner); `chain` is " +
                  "struct(dirs, rlocations, npm_files): the chain's " +
                  "node_modules directories nearest first, as bin-dir paths " +
                  "and as runfiles paths, and the store files the test " +
                  "reaches; the result " +
                  "carries `mode` and `section` (the launcher config's mode " +
                  "and that mode's section), `env`, `files`, `symlinks` and " +
                  "`transitive_files` for the runfiles, and `output_groups`.",
    },
)

TsConfigInfo = provider(
    doc = """A tsconfig.json, the files it extends, its jsx when preserve and
its module when tsgo emits it.

Starlark cannot read the file, so a ts_config target declares what a rule needs
from it before any action runs: the `extends` chain, every file of which becomes
an action input, and the two compiler options that name an output.
""",
    fields = {
        "tsconfig": "File: The tsconfig.json this target declares.",
        "deps_tsconfigs": "depset of File: Every file `tsconfig` extends, transitively.",
        "jsx": "string: \"preserve\" when the chain's effective jsx is " +
               "preserve, so a .tsx emits .jsx as under tsc; \"\" otherwise.",
        "module": "string: the chain's effective module when tsgo emits it " +
                  "-- commonjs, node16, node18, nodenext -- lowercased as " +
                  "tsgo prints it, so a ts_compile declares the ES twin of " +
                  "each .js for the vitest runner; \"\" for an ES kind or " +
                  "preserve.",
    },
)

NpmPackageInfo = provider(
    doc = "Provider for npm package targets.",
    fields = {
        "package_name": "string: npm package name (e.g., 'react').",
        "package_version": "string: npm package version.",
        "peer_id": "string: a filesystem-safe token naming the peer set this resolution was made against, empty for a package pnpm resolved only one way. Two snapshots can share name@version and differ only here, and they are two different dependency graphs, so anything keying a package by name and version alone merges them.",
        "package_dir": "File or None: The package.json file at the root of " +
                       "the extracted package. None on a pnpm workspace " +
                       "member, which was never extracted from a tarball: " +
                       "its compile stages the manifest as built.",
        "package_root": "string: exec-root-relative directory the files in `all_files` hang off -- where `package_dir` sits for an extracted tarball, the member's directory under bazel-bin for a workspace member. A file outside it stages at the package root under its basename.",
        "all_files": "depset of File: every file of this package " +
                     "(package.json, .js, .d.ts, other assets), the files " +
                     "its store tree copies; a member's are its outputs, " +
                     "the manifest as built among them.",
        "transitive_deps": "depset of NpmPackageInfo: Transitive npm dependencies.",
        "store": "NpmStoreInfo: this resolution's store tree and the links " +
                 "beside it (npm/private/store.bzl), one per snapshot, in " +
                 "the lockfile's package.",
    },
)

DevServerInfo = provider(
    doc = """How to start one dev server implementation.

The shipped implementation is Vite (`//vite:dev_server`), the default of
`ts_dev_server(server = ...)`; another is any rule returning this provider.

Two things differ between implementations and neither can be papered over.
A server shipping as an npm package has no `File` to point at -- its executable
is a path under the importer's `node_modules` directory, reached through the
package's link -- so it sets `server_in_tree` and leaves `server_binary` None.
A native binary is the other way round; exactly one of the two must be set.

`config_dialect` names the config the server is handed; only Vite's is
generated. A server need not read all of it, and a field it drops is not always
a field it does without: one taking the serve root from argv instead says so in
`argv`, and one ignoring a field says so in `ignored_config_fields`.
""",
    fields = {
        "server_binary": "File or None: the server executable, for a server " +
                         "that is a build artifact. None when the server " +
                         "ships as an npm package, in which case " +
                         "server_in_tree names it instead.",
        "server_in_tree": "string: the server executable's path under the " +
                          "importer's node_modules directory, for a server " +
                          "that ships as an npm package. Empty when " +
                          "server_binary is set.",
        "argv": "list of string: the command line after the executable. `{config}` expands to the generated config's path and `{root}` to the directory being served; a server taking either somewhere other than where the other one takes it says so here rather than in the launcher.",
        "config_dialect": "string: which config format this server is handed. Only \"vite\" is generated today; a server reading its own format declares its own dialect, and the generator has to learn it before that server can be selected.",
        "runs_in_js_runtime": "bool: True when the executable is JavaScript and the toolchain Node runs it, False for a native binary. A native server still gets the toolchain Node on PATH: one whose plugin host is a Node process is not a Node-free one.",
        "ignored_config_fields": "list of string: dotted config paths this server does not honour, e.g. [\"server.open\"]. A target whose configuration depends on one of these fails at analysis time naming the field and the server, rather than starting a server that quietly does something else.",
        "native_react_refresh": "bool: True when the server applies React Fast Refresh itself. `react_refresh = True` then fails rather than stacking @vitejs/plugin-react on top of a transform that already ran.",
        "runtime_deps": "depset of File: everything the server needs in " +
                        "runfiles beyond the generated config and the " +
                        "importer's node_modules.",
    },
)

NodeModulesInfo = provider(
    doc = """An importer's node_modules: the links a `node_modules` target
declares into the virtual store, one per npm package the importer declares,
the importer above it and the lockfile's hidden hoist
(docs/rules/node-modules.md).""",
    fields = {
        "label": "Label: the node_modules target's, the importer's package " +
                 "and what a message names.",
        "dir": "string: the importer's node_modules directory as a bin-dir " +
               "path, `bazel-out/<cfg>/bin/<package>/node_modules`: the " +
               "parent of every link, which no artifact names.",
        "links": "dict of string -> NpmLinkInfo: per package name, the " +
                 "declared symlink `node_modules/<name>` and the store it " +
                 "enters.",
        "parent": "NodeModulesInfo or None: the importer above's.",
        "hoist": "dict of string -> NpmLinkInfo: the lockfile's hidden " +
                 "hoist, per hoisted name the declared symlink " +
                 "`node_modules/.pnpm/node_modules/<name>` (a " +
                 "`public-hoist-pattern` match's at the root importer's " +
                 "`node_modules/<name>`) and the store it enters; the root " +
                 "importer's `hoist` target's, the same on every importer " +
                 "of its chain.",
    },
)

NpmHoistInfo = provider(
    doc = """A lockfile's hidden hoist, what its `npm_store_hoist` target
returns: the root importer names the target in `hoist`, and every importer
on the chain carries the links as `NodeModulesInfo.hoist`.""",
    fields = {
        "links": "dict of string -> NpmLinkInfo: per hoisted name, the " +
                 "declared symlink and the store it enters.",
    },
)

NpmLinkInfo = provider(
    doc = """One link `node_modules/<name>` into a store tree: an entry of
`NodeModulesInfo.links`, and what a `node_modules_member` target returns for
a workspace member beside the member's own TsInfo and NpmPackageInfo.""",
    fields = {
        "link": "File: the declared symlink `node_modules/<name>`.",
        "store": "NpmStoreInfo: the store the link enters.",
    },
)

BundlerInfo = provider(
    doc = """Information about a JavaScript bundler.

BundlerInfo is the interface behind ts_binary's `bundler` attr. The ruleset
ships no implementation; a consumer writes a rule that returns this provider
and names it there.

Two invocation modes are supported:

Mode 1 — Standard CLI (use_generated_config = False, the default):
  The bundler binary is invoked with:
    --entry <path_to_entry.js>
    --out-dir <output_dir>
    --format esm|cjs|iife
    --external <pkg>         (may be repeated)
    --sourcemap              (flag, no value)
    --config <config_file>   (optional)

Mode 2 — Generated config (use_generated_config = True):
  ts_binary generates a Vite lib-mode vite.config.mjs and invokes the bundler
  with four exec-root-relative arguments: that config, the entry .js, the
  output directory and the stylesheet to create. The binary runs Vite over the
  config with EXEC_ROOT, VITE_ENTRY_PATH and VITE_OUT_DIR set; see
  ts/private/bundle_action.bzl for the outputs it must produce.
""",
    fields = {
        "bundler_binary": "File: The bundler CLI executable.",
        "config_file": "File or None: Optional static bundler config file passed via --config (mode 1 only).",
        "runtime_deps": "depset of File: Additional files needed by the bundler at runtime.",
        "use_generated_config": "bool: When True, ts_binary generates a vite.config.mjs and invokes bundler_binary in mode 2. Default False.",
    },
)
