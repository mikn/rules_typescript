"""Core TypeScript compilation rule: oxc and tsgo over the tsconfig's options.

With emit=True, ts_compile transforms .ts/.tsx into .js + .js.map + .d.ts outputs
in one TsEmit action: oxc's transform for an ES-module program, tsgo's emit
for a CommonJS-shaped one (docs/rules/ts-compile.md § The Module Format). A
program tsgo emits, declared by its ts_config's `module`, gets a second TsEmit:
the ES twin of each .js under <name>.es/, which a vitest test runs in place of
the .js. A .tsx under jsx: preserve emits .jsx, the name tsc gives it, with its
JSX left for the bundler.

JavaScript sources (.js/.mjs/.cjs) are accepted too. They need no transform, so
they are materialised in the output tree unchanged and joined into the type
program: `import "./util.js"` resolves, JSDoc types cross the package boundary,
and `checkJs` in the tsconfig type-checks them.

srcs may span a whole subtree. Every output keeps its package-relative path, so
one target can hold `index.ts` and `nested/helper.ts` together.

The .d.ts are the compilation boundary: a dependent's program reads them and
nothing else of the target, so a change that leaves them byte-identical
recompiles no dependent. They are TsInfo.declarations and the `declarations`
output group, never a default output: TsgoDeclare emits them when a dependent
reads them or the group is requested, and a leaf runs the check alone. A
ts_test under the target's tsconfig is the one dependent that reads the
sources instead, one program with the compile (`package_program`), and a
ts_test's own program emits no declarations (`declarations = False`): nothing
reads a test's .d.ts, so its srcs may hang off any number of roots.

tsgo checks every program under --noEmit, TsgoCheck, a validation in the
_validation output group, against the importer chain `node_modules` names: a
direct npm dep is
the link of the nearest importer that declares it, its closure the store trees
and edge links that link reaches, a `@types/<name>` twin the chain links comes
with it, a member link target brings the member's tree, and every first-party
dep's npm files come along. tsgo walks up from the importing file for a bare
specifier and nothing above a source in the exec root is an output, so tsaction
runs it from a program root holding the action's source inputs at their paths
and the output tree whole, with each importer's node_modules at the importer's
directory, and the declarations, manifest and data of every first-party dep at
or above the target's package laid over that package's sources -- never its
JavaScript, which a program reads through the declarations -- the dep's
package.json as built at the package's path, so the tsconfig's own include
names the srcs and the deps' declarations and every import resolves as it does
over a pnpm install, the package's own name through the nearest manifest
included. The check runs during `bazel build` and blocks no dependent; a type
error fails the build.
Under --//ts:declarations=tsgo a second run of the same program, TsgoDeclare,
emits the .d.ts with the declaration shape on its command line, so the
written tsconfig carries none and the check runs no declaration transformer;
a chain that sets isolatedDeclarations keeps declaration on, which the option
requires, and its check reports an unannotated export as `tsc -p` does.
The linter the root module's ts.lint() names runs over the same sources as a
second validation action, TsLint. The emit reads the same program root when
tsgo emits the JavaScript.

The default emit=False retains source files for transforming runtimes and validation,
without JavaScript or declaration emission. Every compiler option is the
tsconfig's; the emit knobs are the build flags
//ts:declarations (tsgo|oxc),
//ts:source_map, //ts:declaration_map and //ts:lib_check. Each action is a
function under ts/private/actions/; compile_program declares the outputs, calls
them in order and builds the providers, for ts_compile and for the ts_test rule
over the same attributes.
"""

load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")
load("//ts/private:node_modules.bzl", "importer_chain")
load(
    "//ts/private:providers.bzl",
    "NodeModulesInfo",
    "NpmLinkInfo",
    "NpmPackageInfo",
    "TsConfigInfo",
    "TsInfo",
    "label_text",
)
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE")
load(
    "//ts/private:toolchain.bzl",
    "OXC_TOOLCHAIN_TYPE",
    "TOOLS_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "get_oxc_toolchain",
)
load("//ts/private/actions:emit.bzl", "emit_action")
load("//ts/private/actions:format.bzl", "FORMAT_ATTR", "FormatConfigInfo", "format_action")
load("//ts/private/actions:lint.bzl", "LintConfigInfo", "lint_action")
load("//ts/private/actions:manifest.bzl", "manifest_action")
load(
    "//ts/private/actions:tsconfig.bzl",
    "tsconfig_action",
    "write_baseline_tsconfig",
)
load(
    "//ts/private/actions:tsgo.bzl",
    "npm_hub_entry",
    "npm_hub_label",
    "ownership_manifest",
    "tsgo_check",
    "tsgo_declare",
)

_TS_EXTENSIONS = ["ts", "tsx"]

_JS_EXTENSIONS = ["js", "mjs", "cjs"]

_UNSHAPED_TS_EXTENSIONS = ["mts", "cts"]

# tsc's own naming for the declaration it emits from a JavaScript source.
_JS_DECLARATION_EXTENSION = {
    "js": ".d.ts",
    "mjs": ".d.mts",
    "cjs": ".d.cts",
}

_DECLARATION_SUFFIXES = (".d.ts", ".d.mts", ".d.cts")

_INSTRUMENTED_EXTENSIONS = [
    "ts",
    "tsx",
    "mts",
    "cts",
    "js",
    "jsx",
    "mjs",
    "cjs",
]

def _is_dts_source(f):
    """Returns True if the file is a declaration file."""
    return f.basename.endswith(_DECLARATION_SUFFIXES)

def _package_relative_path(f, pkg):
    """The path of a src relative to the target's package, extension intact."""
    p = f.short_path
    if p.startswith("../"):
        # An external-repo file: ../<repo name>/<rest>.
        parts = p.split("/", 2)
        if len(parts) == 3:
            p = parts[2]
    if pkg and p.startswith(pkg + "/"):
        p = p[len(pkg) + 1:]
    return p

def _strip_ts_extension(p):
    for ext in (".tsx", ".ts"):
        if p.endswith(ext):
            return p[:-len(ext)]
    return p

def _package_relative_stem(f, pkg):
    """The package-relative path with the TypeScript extension stripped."""
    return _strip_ts_extension(_package_relative_path(f, pkg))

def _source_root(f, pkg):
    """The exec-root-relative directory the package-relative path hangs off.

    A checked-in source gives the package directory; a generated one gives the
    bin directory plus the package. oxc's --strip-dir-prefix takes a single
    value, so two srcs with different roots cannot share one invocation.
    """
    rel = _package_relative_path(f, pkg)
    p = f.path
    if p == rel:
        return ""
    if p.endswith("/" + rel):
        return p[:len(p) - len(rel) - 1]
    return f.dirname

def _classify_srcs(ctx):
    """Splits srcs into TypeScript, JavaScript, declaration and data sets."""
    compile_srcs = []
    js_srcs = []
    passthrough_dts = []
    data_srcs = []
    for f in ctx.files.srcs:
        if f.is_directory:
            fail(
                "ts_compile: '{}' on {} is a directory.\n".format(
                    f.short_path,
                    ctx.label,
                ) +
                "srcs declares one output per file at analysis time, and a " +
                "directory has no file list until its action has run.\nA " +
                "tree of already-compiled output -- ts_codegen(out_dir = " +
                "...) -- belongs in deps, where it is staged whole and " +
                "reached through the tsconfig's `paths`.",
            )
        if _is_dts_source(f):
            passthrough_dts.append(f)
        elif f.extension in _TS_EXTENSIONS:
            compile_srcs.append(f)
        elif f.extension in _JS_EXTENSIONS:
            js_srcs.append(f)
        elif f.extension == "jsx":
            fail(
                "ts_compile: '{}' on {} is a .jsx file. ".format(
                    f.short_path,
                    ctx.label,
                ) +
                "JavaScript is staged unchanged, and tsc would transform the " +
                "JSX in one under every jsx mode but preserve.\nRename it to " +
                ".tsx -- TypeScript accepts the JavaScript in it unchanged " +
                "-- or drop the JSX and call it .js.",
            )
        elif f.extension in _UNSHAPED_TS_EXTENSIONS:
            fail(
                "ts_compile: '{}' on {} is a .{} file, and the rule ".format(
                    f.short_path,
                    ctx.label,
                    f.extension,
                ) +
                "emits .js and .d.ts from .ts alone.\nRename it to .ts, or " +
                "leave it out of srcs.",
            )
        else:
            data_srcs.append(f)
    return compile_srcs, js_srcs, passthrough_dts, data_srcs

def _types_twin(name):
    if name.startswith("@types/"):
        return None
    if name.startswith("@"):
        return "@types/" + name[1:].replace("/", "__")
    return "@types/" + name

def _manifest_of(importer):
    return "/".join([p for p in [importer.label.package, "package.json"] if p])

def _bin_dir(ctx, label):
    return "/".join([
        p
        for p in [ctx.bin_dir.path, label.workspace_root, label.package]
        if p
    ])

# A dep at or above this package shares its directory with the sources: the
# program root lays the dep's declarations and data over them.
def _encloses(dep, target):
    if dep.workspace_root != target.workspace_root:
        return False
    return dep.package == "" or dep.package == target.package or \
           target.package.startswith(dep.package + "/")

def _same_tsconfig(ctx, info):
    return info.tsconfig != None and info.tsconfig == ctx.file.tsconfig

def _importer_chain(ctx, packages):
    if not ctx.attr.node_modules:
        if packages:
            fail(("{}: the closure holds npm packages ({}) and " +
                  "`node_modules` names no importer whose chain resolves " +
                  "them; set it to the `node_modules` target of the nearest " +
                  "lockfile importer at or above this package, as Gazelle " +
                  "writes it.").format(
                ctx.label,
                ", ".join(sorted([info.package_name for info in packages])),
            ))
        return []
    return importer_chain(ctx.attr.node_modules[NodeModulesInfo])

def _importer_linking(chain, name):
    for importer in chain:
        if name in importer.links:
            return importer
    return None

def _resolve_on_chain(ctx, chain, dep):
    name = dep.info.package_name
    importer = _importer_linking(chain, name)
    if importer == None:
        fail(("{}: '{}' in deps is linked by no importer on the chain {}; " +
              "declare it in {} and run `pnpm install " +
              "--lockfile-only`.").format(
            ctx.label,
            name,
            " -> ".join([label_text(i.label) for i in chain]),
            _manifest_of(chain[0]),
        ))
    link = importer.links[name]
    if link.store.key != dep.info.store.key:
        fail(("{}: '{}' in deps is {} ({}) and the importer {} links {}: a " +
              "target resolves its importer's resolution; name {} in " +
              "deps.").format(
            ctx.label,
            name,
            dep.info.store.key,
            dep.label,
            label_text(importer.label),
            link.store.key,
            npm_hub_label(dep.info, importer.label.package),
        ))
    return link

def _bare_view_message(ctx, name):
    return ("{}: '{}' in deps is the hub's view of a workspace member; name " +
            "the link target of the importer that links it instead, " +
            "`//<importer>:node_modules/{}`, as Gazelle writes it.").format(
        ctx.label,
        name,
        name,
    )

def _npm_closure(direct, dep_sets):
    """Every distinct resolution the direct npm deps and the first-party deps'
    closures reach, the direct ones first, by store key."""
    seen = {}
    packages = []
    closure = depset(transitive = dep_sets, order = "postorder").to_list()
    for info in direct + closure:
        for reached in [info] + info.transitive_deps.to_list():
            if reached.store.key not in seen:
                seen[reached.store.key] = True
                packages.append(reached)
    return packages

def compile_program(
        ctx,
        es_modules = False,
        es_twins = False,
        package_program = False,
        declarations = True):
    """Registers the actions over ctx's srcs, deps, tsconfig and node_modules.

    The body of ts_compile and of ts_test: one attrs dict, one set of action
    functions. `es_modules` emits the program as ES modules whatever its
    tsconfig's module, the vitest runner's program; `es_twins` adds, to a
    program tsgo emits, the ES twin of each .js for the vitest tests that
    depend on it; `package_program` checks every dep under the target's
    tsconfig from its sources, one program with the package's compile, the
    ts_test rule's; `declarations = False` declares no .d.ts and registers
    no declaration emit, a test's program. Returns struct(outputs, js,
    emitted, importers, npm_files, packages, transitive_js, transitive_data,
    es_twins, info, instrumented, output_groups): `emitted` each compiled
    src's outputs, `importers` the chain's NodeModulesInfo nearest first and
    `npm_files` the store files the program reaches.
    """
    emit = ctx.attr.emit
    declarations = declarations and emit
    oxc = get_oxc_toolchain(ctx) if emit else None
    pkg = ctx.label.package

    compile_srcs, js_srcs, passthrough_dts, data_srcs = _classify_srcs(ctx)

    dep_npm_package_sets = []
    dep_npm_file_sets = []
    transitive_js_sets = []
    runtime_source_sets = []
    transitive_js_map_sets = []
    transitive_data_sets = []
    transitive_es_twins_sets = []

    direct_npm_infos = []
    published = []
    npm_links = []
    direct_labels = []
    owner_sets = []
    held_as_sources = {}
    overlays = {}
    dep_manifests = []
    joined_source_sets = []

    # A dep reached through the store is in the program there; a copy of its
    # files at their exec paths would duplicate every module.
    for dep in ctx.attr.deps:
        info = dep[TsInfo]
        if info.transitive_runtime_sources:
            runtime_source_sets.append(
                dep[NpmPackageInfo].store.transitive if NpmPackageInfo in dep else info.transitive_runtime_sources,
            )
        dep_npm_package_sets.append(info.npm_packages)
        if NpmLinkInfo in dep:
            direct_npm_infos.append(dep[NpmPackageInfo])
            npm_links.append(dep[NpmLinkInfo])
            continue
        if NpmPackageInfo in dep:
            npm_info = dep[NpmPackageInfo]
            if npm_info.package_dir == None:
                fail(_bare_view_message(ctx, npm_info.package_name))
            direct_npm_infos.append(npm_info)
            published.append(struct(label = dep.label, info = npm_info))
            continue
        if package_program and _same_tsconfig(ctx, info):
            joined_source_sets.append(info.sources)
            held_as_sources[label_text(dep.label)] = True
        transitive_js_sets.append(info.transitive_js)
        transitive_js_map_sets.append(info.transitive_js_maps)
        transitive_data_sets.append(info.transitive_data)
        transitive_es_twins_sets.append(info.transitive_es_twins)
        dep_npm_file_sets.append(info.npm_files)
        direct_labels.append(label_text(dep.label))
        owner_sets.append(info.owners)
        if _encloses(dep.label, ctx.label):
            overlays[_bin_dir(ctx, dep.label)] = True
            if info.manifest:
                dep_manifests.append(info.manifest)

    # A direct package resolves along the chain nearest first, pnpm's walk-up;
    # the @types twin an importer links beside it is the package's to declare.
    packages = _npm_closure(direct_npm_infos, dep_npm_package_sets)
    chain = _importer_chain(ctx, packages)
    npm_declared = {
        info.package_name: info.store.key
        for info in direct_npm_infos
    }
    for dep in published:
        npm_links.append(_resolve_on_chain(ctx, chain, dep))
        twin = _types_twin(dep.info.package_name)
        twin_importer = _importer_linking(chain, twin) if twin else None
        if twin_importer != None:
            npm_links.append(twin_importer.links[twin])
            npm_declared[twin] = twin_importer.links[twin].store.key
    for info in packages:
        hoist = chain[0].hoist
        if info.package_name in hoist.links:
            npm_links.append(hoist.links[info.package_name])
        elif info.package_name in hoist.members:
            npm_links.append(NpmLinkInfo(
                link = hoist.members[info.package_name],
                store = info.store,
            ))
    npm_files = depset(
        [entry.link for entry in npm_links],
        transitive = (
            [entry.store.transitive for entry in npm_links] + dep_npm_file_sets
        ),
    )
    own_importers = [importer.dir for importer in chain]
    dependency_importers = {}
    for record in depset(transitive = owner_sets).to_list():
        for directory in record.importers:
            if directory not in own_importers:
                dependency_importers[directory] = True
    importers = dependency_importers.keys() + own_importers

    declared_keys = {key: True for key in npm_declared.values()}
    npm_reachable = [
        npm_hub_entry(info)
        for info in packages
        if info.store.key not in declared_keys
    ]

    dep_dts_depset = depset(
        transitive = [
            record.type_inputs
            for record in depset(transitive = owner_sets).to_list()
            if record.label not in held_as_sources
        ],
        order = "postorder",
    )

    # Starlark cannot read the file to follow its extends chain, so a ts_config
    # target declares it and every file in it is an action input.
    baseline_file = write_baseline_tsconfig(ctx)
    tsconfig_chain = [baseline_file]
    declared_jsx = ""
    declared_module = ""
    if ctx.file.tsconfig:
        tsconfig_chain.append(ctx.file.tsconfig)
        if TsConfigInfo in ctx.attr.tsconfig:
            config_info = ctx.attr.tsconfig[TsConfigInfo]
            tsconfig_chain += config_info.deps_tsconfigs.to_list()
            declared_jsx = config_info.jsx
            declared_module = config_info.module
    tsx_extension = ".jsx" if declared_jsx == "preserve" else ".js"

    declarations_flag = ctx.attr._declarations[BuildSettingInfo].value
    oxc_emits_dts = declarations and declarations_flag == "oxc"
    tsgo_emits_dts = declarations and declarations_flag == "tsgo"
    source_map = ctx.attr._source_map[BuildSettingInfo].value
    declaration_map = ctx.attr._declaration_map[BuildSettingInfo].value
    checkers = ctx.attr._checkers[BuildSettingInfo].value

    if declaration_map and declarations_flag == "oxc":
        fail(
            "ts_compile: --//ts:declaration_map needs the tsgo declaration " +
            "emit, and --//ts:declarations=oxc takes it away.\noxc writes " +
            "declarations syntactically and emits no map for them.\nBuild " +
            "with one flag or the other.",
        )

    # One output per src at its package-relative path.
    out_base = _bin_dir(ctx, ctx.label)

    emit_roots = {}
    emit_outputs = []
    emitted = {}
    js_outputs = []
    js_map_outputs = []
    dts_outputs = []
    dts_map_outputs = []
    twin_pairs = []
    twins_dir = "{}.es".format(ctx.label.name)

    for src in compile_srcs if emit else []:
        stem = _package_relative_stem(src, pkg)
        emit_roots[_source_root(src, pkg)] = True

        js_extension = tsx_extension if src.extension == "tsx" else ".js"
        js_out = ctx.actions.declare_file(stem + js_extension)
        js_outputs.append(js_out)
        emit_outputs.append(js_out)
        emitted[src] = [js_out]
        if es_twins and declared_module:
            twin_pairs.append((js_out, ctx.actions.declare_file(
                "{}/{}".format(twins_dir, stem + js_extension),
            )))
        if source_map:
            js_map_out = ctx.actions.declare_file(stem + js_extension + ".map")
            js_map_outputs.append(js_map_out)
            emit_outputs.append(js_map_out)
            emitted[src].append(js_map_out)
        if declarations:
            dts_out = ctx.actions.declare_file(stem + ".d.ts")
            dts_outputs.append(dts_out)
            if oxc_emits_dts:
                emit_outputs.append(dts_out)
            if declaration_map:
                dts_map_outputs.append(
                    ctx.actions.declare_file(stem + ".d.ts.map"),
                )

    # A .mjs beside its .d.mts leaves the program (tsc keeps the higher-priority
    # extension), so tsgo writes no declaration for it: checked_in_dts.
    checked_in_dts = {
        _package_relative_path(d, pkg): True
        for d in passthrough_dts
    }
    js_passthrough = []
    for src in js_srcs:
        rel = _package_relative_path(src, pkg)
        staged = ctx.actions.declare_file(rel)
        ctx.actions.symlink(output = staged, target_file = src)
        js_passthrough.append(staged)
        stem = rel[:-(len(src.extension) + 1)]
        dts_rel = stem + _JS_DECLARATION_EXTENSION[src.extension]
        if tsgo_emits_dts and dts_rel not in checked_in_dts:
            dts_outputs.append(ctx.actions.declare_file(dts_rel))
            if declaration_map:
                dts_map_outputs.append(
                    ctx.actions.declare_file(dts_rel + ".map"),
                )

    data_staged = []
    manifest = None
    for src in data_srcs:
        rel = _package_relative_path(src, pkg)
        staged = ctx.actions.declare_file(rel)
        ctx.actions.symlink(output = staged, target_file = src)
        data_staged.append(staged)
        if rel == "package.json":
            if emit:
                manifest = ctx.actions.declare_file(
                    "{}.package.json".format(ctx.label.name),
                )
                manifest_action(ctx, src, manifest, tsx_extension)
            else:
                manifest = src

    # JavaScript srcs whose declarations are all checked in leave tsgo nothing
    # to write: a program to check, not one to emit from.
    tsgo_emits_dts = tsgo_emits_dts and bool(dts_outputs)

    runtime_sources = [] if emit else compile_srcs
    as_built = [manifest] if manifest else []
    all_outputs = (
        runtime_sources + js_outputs + js_map_outputs + js_passthrough + data_staged + as_built
    )

    program_srcs = compile_srcs + js_srcs
    check_srcs = compile_srcs + js_srcs + passthrough_dts
    joined = depset(transitive = joined_source_sets).to_list()

    direct_dts = depset(dts_outputs + passthrough_dts, order = "postorder")
    owners = depset(
        [struct(
            label = label_text(ctx.label),
            files = depset(
                check_srcs + dts_outputs + data_staged + as_built,
            ),
            declarations = direct_dts,
            type_inputs = direct_dts if emit else depset(check_srcs),
            importers = tuple([importer.dir for importer in chain]),
        )],
        transitive = owner_sets,
    )

    # tsc reads a JSON src on its own: an import resolves to it, and the nearest
    # package.json decides a module's format and the package's own name.
    json_srcs = [f for f in data_srcs if f.extension == "json"]

    # A dep's .json is typed from the file too: the closure's join the program
    # beside the declarations, and an import of one resolves in the sandbox.
    dep_json = [
        f
        for f in depset(transitive = transitive_data_sets).to_list()
        if f.extension == "json"
    ]
    lint = ctx.attr._lint[LintConfigInfo]
    needs_config = program_srcs or (lint.binary and check_srcs and any(["{tsconfig}" in arg for arg in lint.args]))
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if needs_config and not tsgo_toolchain_info:
        fail(("ts_compile: {} needs a tsgo toolchain, and none is registered." +
              "\nThe tsconfig the actions read, and the target and jsx oxc " +
              "transforms with, come from `tsgo --showConfig`.\nAdd to " +
              "MODULE.bazel:\n    register_toolchains(" +
              "\"@rules_typescript//ts/toolchain:all\")").format(ctx.label))

    tsconfig = None
    options_file = None
    if needs_config:
        tsgo = tsgo_toolchain_info.tsgo_info

        declare_root = None
        if tsgo_emits_dts and program_srcs:
            roots = {}
            for src in program_srcs:
                roots[_source_root(src, pkg)] = True
            root_list = sorted(roots.keys())
            if len(root_list) > 1:
                fail(
                    "ts_compile: srcs on {} hang off {} different ".format(
                        ctx.label,
                        len(root_list),
                    ) +
                    "roots, and one declaration emit has one rootDir:\n  " +
                    "\n  ".join([r or "the exec root" for r in root_list]) +
                    "\nA target may hold a whole subtree, but not a mix of " +
                    "checked-in and generated sources. Put the generated " +
                    "sources in their own ts_compile target and depend on " +
                    "it, or build with --//ts:declarations=oxc, which emits " +
                    "nothing from tsgo.",
                )
            declare_root = root_list[0]

        written = tsconfig_action(
            ctx,
            tsgo = tsgo,
            check_srcs = check_srcs + joined,
            tsconfig_chain = tsconfig_chain,
            baseline_file = baseline_file,
            dep_dts = dep_dts_depset,
            declared_jsx = declared_jsx,
            declared_module = declared_module,
            types_deps = sorted([
                info.package_name[len("@types/"):]
                for info in direct_npm_infos
                if info.package_name.startswith("@types/")
            ]),
            isolated_declarations = oxc_emits_dts,
            lib_check = ctx.attr._lib_check[BuildSettingInfo].value,
            emit = emit,
        )
        tsconfig = written.tsconfig
        options_file = written.options

    validation_outputs = []
    format_stamp = format_action(ctx, ctx.attr._format[FormatConfigInfo], ctx.files.srcs)
    if format_stamp:
        validation_outputs.append(format_stamp)
    program_inputs = check_srcs + joined + json_srcs + dep_json + dep_manifests
    if program_srcs:
        if emit and compile_srcs:
            emit_action(
                ctx,
                oxc = oxc,
                tsgo = tsgo,
                srcs = compile_srcs,
                roots = sorted(emit_roots.keys()),
                outputs = emit_outputs,
                out_base = out_base,
                tsconfig = tsconfig,
                chain = tsconfig_chain,
                importers = importers,
                overlays = sorted(overlays.keys()),
                manifests = dep_manifests,
                program_inputs = program_inputs,
                dep_dts = dep_dts_depset,
                npm_files = npm_files,
                options_file = options_file,
                scratch = "{}/{}.emit".format(tsconfig.dirname, ctx.label.name),
                source_map = source_map,
                emit_dts = oxc_emits_dts,
                es_modules = es_modules,
            )
        if twin_pairs:
            emit_action(
                ctx,
                oxc = oxc,
                tsgo = tsgo,
                srcs = compile_srcs,
                roots = sorted(emit_roots.keys()),
                outputs = [twin for _, twin in twin_pairs],
                out_base = "{}/{}".format(out_base, twins_dir),
                tsconfig = tsconfig,
                chain = tsconfig_chain,
                importers = importers,
                overlays = sorted(overlays.keys()),
                manifests = dep_manifests,
                program_inputs = program_inputs,
                dep_dts = dep_dts_depset,
                npm_files = npm_files,
                options_file = options_file,
                scratch = "{}/{}.emit".format(tsconfig.dirname, twins_dir),
                source_map = False,
                emit_dts = False,
                es_modules = True,
            )
        ownership = ownership_manifest(
            ctx,
            own = check_srcs + json_srcs,
            direct = direct_labels,
            owners = owners,
            npm_declared = [
                struct(name = name, key = npm_declared[name])
                for name in sorted(npm_declared)
            ],
            npm_reachable = npm_reachable,
        )
        validation_outputs.append(tsgo_check(
            ctx,
            tsgo = tsgo,
            tsconfig = tsconfig,
            importers = importers,
            overlays = sorted(overlays.keys()),
            manifests = dep_manifests,
            srcs = program_inputs,
            chain = tsconfig_chain,
            dep_dts = dep_dts_depset,
            npm_files = npm_files,
            ownership = ownership,
            checkers = checkers,
        ))
        if tsgo_emits_dts:
            tsgo_declare(
                ctx,
                tsgo = tsgo,
                tsconfig = tsconfig,
                importers = importers,
                overlays = sorted(overlays.keys()),
                manifests = dep_manifests,
                srcs = program_inputs,
                chain = tsconfig_chain,
                dep_dts = dep_dts_depset,
                npm_files = npm_files,
                outputs = dts_outputs + dts_map_outputs,
                out_dir = out_base,
                root_dir = declare_root,
                declaration_map = declaration_map,
                checkers = checkers,
            )

    if lint.binary and check_srcs:
        validation_outputs.append(lint_action(
            ctx,
            lint = lint,
            srcs = check_srcs,
            tsconfig = tsconfig,
            inputs = program_inputs,
            chain = tsconfig_chain,
            dep_dts = dep_dts_depset,
            npm_files = npm_files,
            importers = importers,
            overlays = sorted(overlays.keys()),
            manifests = dep_manifests,
        ))

    direct_runtime_sources = depset(runtime_sources, order = "postorder")
    transitive_runtime_sources = depset(
        runtime_sources,
        transitive = runtime_source_sets,
        order = "postorder",
    )
    direct_js = depset(js_outputs + js_passthrough, order = "postorder")
    direct_js_map = depset(js_map_outputs, order = "postorder")

    transitive_js = depset(
        js_outputs + js_passthrough,
        transitive = transitive_js_sets,
        order = "postorder",
    )
    transitive_js_map = depset(
        js_map_outputs,
        transitive = transitive_js_map_sets,
        order = "postorder",
    )
    transitive_data = depset(
        data_staged,
        transitive = transitive_data_sets,
        order = "postorder",
    )
    transitive_es_twins = depset(
        twin_pairs,
        transitive = transitive_es_twins_sets,
    )

    info = TsInfo(
        js = direct_js,
        runtime_sources = direct_runtime_sources,
        runtime_source_owners = depset(
            [label_text(ctx.label)] if runtime_sources else [],
            transitive = [dep[TsInfo].runtime_source_owners for dep in ctx.attr.deps],
        ),
        transitive_runtime_sources = transitive_runtime_sources,
        js_maps = direct_js_map,
        declarations = direct_dts,
        data = depset(data_staged, order = "postorder"),
        manifest = manifest,
        sources = depset(check_srcs, order = "postorder"),
        tsconfig = ctx.file.tsconfig,
        transitive_js = transitive_js,
        transitive_js_maps = transitive_js_map,
        transitive_data = transitive_data,
        transitive_es_twins = transitive_es_twins,
        npm_packages = depset(
            direct_npm_infos,
            transitive = dep_npm_package_sets,
            order = "postorder",
        ),
        npm_files = npm_files,
        owners = owners,
    )

    output_groups = {}
    if declarations:
        output_groups["declarations"] = depset(
            dts_map_outputs,
            transitive = [direct_dts],
        )

    # The tsconfig the compiler read, for a test comparing the build's
    # resolution with the editor's; a target with no program generates none.
    if tsconfig:
        output_groups["tsconfig"] = depset([tsconfig])
    if validation_outputs:
        output_groups["_validation"] = depset(validation_outputs)

    return struct(
        outputs = all_outputs + passthrough_dts,
        js = js_outputs + js_passthrough,
        runtime_sources = runtime_sources,
        transitive_runtime_sources = transitive_runtime_sources,
        emitted = emitted,
        importers = chain,
        npm_files = npm_files,
        packages = packages,
        transitive_js = transitive_js,
        transitive_data = transitive_data,
        es_twins = transitive_es_twins,
        info = info,
        # The runner reports on the compiled .js; a baseline naming the .ts
        # would be a second name for the same code, with no lines at all.
        instrumented = coverage_common.instrumented_files_info(
            ctx,
            source_attributes = ["srcs"],
            dependency_attributes = ["deps"],
            extensions = _INSTRUMENTED_EXTENSIONS,
            baseline_coverage_files = [],
        ),
        output_groups = output_groups,
    )

def _ts_compile_impl(ctx):
    program = compile_program(ctx, es_twins = True)

    # This target's own outputs; a dep's reach a consumer through TsInfo.
    return [
        DefaultInfo(files = depset(program.outputs)),
        program.info,
        program.instrumented,
        OutputGroupInfo(**program.output_groups),
    ]

TS_COMPILE_ATTRS = {
    "emit": attr.bool(
        default = False,
        doc = "Opt in to JavaScript and declaration emission for consumers that require built files. By default, publish TypeScript sources and retain validation.",
    ),
    "srcs": attr.label_list(
        doc = """The package's files.

.ts / .tsx      compiled; one .js (+ .js.map, + .d.ts) output each, from oxc
                for an ES-module program and from tsgo for a CommonJS-shaped
                one, as the tsconfig's `module` says. A program tsgo emits,
                declared by its ts_config's `module`, also gets the ES twin
                of each .js under <name>.es/, which a vitest test runs in
                place of the .js. A .tsx under jsx: preserve emits .jsx
                (+ .jsx.map), the name tsc gives it, when the tsconfig's
                ts_config declares that value.
.js / .mjs/.cjs staged into the output tree unchanged and added to the type
                program. allowJs is set for them, so JSDoc types cross the
                package boundary; set checkJs in the tsconfig to have them
                type-checked. Under --//ts:declarations=tsgo each one also gets
                a declaration (.d.ts / .d.mts / .d.cts), the same as tsc,
                unless srcs already holds that file.
.d.ts / .d.mts / .d.cts
                declarations: type context for the check, passed straight
                through to consumers. One with no top-level import or export
                declares globals, and those are in scope in this target's own
                program; a consumer that wants them names the file in its own
                tsconfig `types`. A .d.mts is the declaration of the .mjs of
                the same stem, whether or not that .mjs is in srcs: "./x.mjs"
                resolves to x.d.mts, and a .mjs listed beside its .d.mts is
                staged but leaves the type program, as under tsc, so the
                checked-in file is its only declaration.
.json           staged, and a tsgo input: an import of it resolves to the file
                and is typed from its contents under resolveJsonModule, which
                bundler resolution implies, and the nearest package.json decides
                a module's format and the package's own name. The package.json
                at the package's root is also written as built, every
                source-file target rewritten to the emitted file, as
                <name>.package.json: the manifest a dependent's program root
                lays at the package's path and a member's store tree copies. A
                test's runfiles hold the src as written.
anything else   staged into the output tree unchanged at its package-relative
                path, so the compiled module beside it reaches it by the same
                relative path at run time; never a tsgo input. A consumer gets
                the closure as TsInfo.transitive_data. A .mts or .cts is
                refused: the rule emits .js and .d.ts from .ts alone.

Paths are kept relative to the target's package, so srcs may span a subtree.
""",
        allow_files = True,
        mandatory = True,
    ),
    "deps": attr.label_list(
        doc = """What this target imports: ts_compile, ts_codegen or
ts_npm_package targets, each providing TsInfo.

An npm dep reaches tsgo through the link of the nearest importer on the
`node_modules` chain that declares it, under its package name; a first-party
dep through its declarations, staged under bazel-bin at the paths the
tsconfig's `paths` and their bin-dir twins reach, or through a relative
import -- on a ts_test, a dep under the test's tsconfig through its sources
instead; a workspace member through its importer's link target,
`//<importer>:node_modules/<name>`; the package's own name, from a test or a
package below it, through the dep's manifest as built, which the program root
lays at the package's path, the dep being the package's ts_compile.""",
        providers = [TsInfo],
    ),
    "node_modules": attr.label(
        doc = """The `node_modules` target of the nearest lockfile importer at
or above this package: the chain a direct npm dep resolves along, nearest
importer first, as pnpm's walk-up from the importing file. The link whose
store is the dep's resolution is the one the program reads; a name no
importer on the chain declares, or one declared at another version, fails
analysis. Required when the closure holds an npm package, a first-party
dep's included: its declarations import the packages it declared, and the
walk up from them ends at the chain's root. Gazelle writes it on every
target.""",
        providers = [NodeModulesInfo],
    ),
    "tsconfig": attr.label(
        doc = """The project's own tsconfig.json: where every compiler option
comes from.

Either a .json file or a ts_config target, which additionally declares the
files the tsconfig `extends`, with `jsx = "preserve"` that a .tsx emits .jsx,
and with `module` the kind tsgo emits, so the ES twins exist; the rule names
its outputs before any action reads the file, and the TsConfig action fails a
target when a declaration and the file disagree. The file is referenced where
it lives, not copied, so relative paths inside it keep resolving against the
directory they were written for.

The action's tsconfig extends the ruleset's baseline (strict, module Preserve,
target es2022, jsx react-jsx, skipLibCheck, esModuleInterop) and then this
file, so every key the file or its own extends chain mentions wins and only the
keys it says nothing about fall back to the baseline. Over both, tsaction sets
the keys Bazel owns -- rootDirs, the emit shape, `include` and `files` --
rewrites `paths` to the source and bin-dir twins of each value, and lists each
path-shaped `types` entry as a root file at its staged path; a `types` entry
naming a package resolves through the importer chain. oxc transforms with the
target, jsx and jsxImportSource tsgo reads from the same chain, and the
chain's `module` decides whether oxc or tsgo emits the JavaScript.

Without a tsconfig the baseline alone is the program's options.

moduleResolution the baseline never asserts: TypeScript couples it to `module`
and tsgo derives the resolver from whichever `module` wins, which is Bundler
for all of them but Node16/NodeNext.""",
        allow_single_file = [".json"],
    ),
    "_format": FORMAT_ATTR,
    "_lint": attr.label(
        default = Label("//ts:lint"),
        providers = [LintConfigInfo],
    ),
    "_declarations": attr.label(default = Label("//ts:declarations")),
    "_source_map": attr.label(default = Label("//ts:source_map")),
    "_declaration_map": attr.label(default = Label("//ts:declaration_map")),
    "_lib_check": attr.label(default = Label("//ts:lib_check")),
    "_checkers": attr.label(default = Label("//ts:checkers")),
}

TS_COMPILE_TOOLCHAINS = [
    OXC_TOOLCHAIN_TYPE,
    TOOLS_TOOLCHAIN_TYPE,
    config_common.toolchain_type(TSGO_TOOLCHAIN_TYPE, mandatory = False),
    config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
]

ts_compile = rule(
    implementation = _ts_compile_impl,
    attrs = TS_COMPILE_ATTRS,
    toolchains = TS_COMPILE_TOOLCHAINS,
    doc = """Publishes TypeScript sources with tsgo validation by default.
Set emit=True to produce JavaScript and declarations.

With emit=True, produces one .js (+ .js.map under --//ts:source_map) and one .d.ts
per .ts/.tsx input -- .jsx and .jsx.map for a .tsx under jsx: preserve, as tsc
names them -- and stages every other src -- JavaScript, JSON, anything -- into
the output tree as-is. Output paths stay relative to the target's package, so
srcs may span a subtree.

The .d.ts are the compilation boundary: a dependent's program reads them and
nothing else of the target, a ts_test under the target's tsconfig apart, which
reads the sources. They are TsInfo.declarations and the
`declarations` output group, not a default output: a leaf's `bazel build`
runs the check alone, and the declarations are emitted when a dependent reads
them or `--output_groups=declarations` asks.

tsgo's check is a validation action in the _validation output group on every
target: it runs during `bazel build`, fails it on a type error and blocks no
dependent. --//ts:declarations decides who emits the .d.ts. Under "tsgo" (the
default) a second tsgo run emits them from the full program; under "oxc" oxc
emits them syntactically, which requires an explicit type on every export.
--//ts:declaration_map adds a .d.ts.map beside each declaration under the
tsgo emit; --//ts:lib_check checks the program's .d.ts closure too.

Compiler options come from the ruleset's baseline and from `tsconfig`, read
through `tsgo --showConfig`. npm packages reach tsgo through the importer
chain `node_modules` names, one link per name an importer declares.
""",
)
