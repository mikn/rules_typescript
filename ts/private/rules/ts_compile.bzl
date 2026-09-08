"""Core TypeScript compilation rule using oxc-bazel.

ts_compile transforms .ts/.tsx source files into .js + .js.map + .d.ts outputs
using the oxc-bazel CLI as a Bazel action. A .tsx under jsx: preserve emits
.jsx, the name tsc gives it, with its JSX left for the bundler.

JavaScript sources (.js/.mjs/.cjs) are accepted too. They need no transform, so
they are materialised in the output tree unchanged and joined into the type
program: `import "./util.js"` resolves, JSDoc types cross the package boundary,
and `checkJs` in the tsconfig type-checks them.

srcs may span a whole subtree. Every output keeps its package-relative path, so
one target can hold `index.ts` and `nested/helper.ts` together.

The .d.ts output is the compilation boundary artifact: downstream targets
depend only on .d.ts files, so Bazel's content-based caching means that if a
dep's .d.ts doesn't change (e.g. because an internal implementation detail
changed but the public API did not), dependents are not recompiled.

tsgo checks the program -- and emits its declarations under declarations =
"tsgo" -- against a node_modules forest: the target's npm deps, their closures,
the @types/* package paired with each and every first-party dep's npm closure,
laid out as node_modules.bzl lays out a runtime tree. tsgo walks up from the
importing file for a bare specifier and nothing above a source in the exec root
is an output, so tsaction runs it from a program root that mirrors the exec
root with the forest at its node_modules, and every import resolves as it does
over a pnpm install. Under --//ts:declarations=oxc the check is a validation
action in the _validation output group: it runs during `bazel build` and does
not block downstream compilation. The linter the root module's ts.lint() names
runs over the same sources as a second validation action, TsLint.

The rule has three attributes: srcs, deps and tsconfig. Every compiler option is
the tsconfig's; the emit knobs are the build flags //ts:declarations (tsgo|oxc),
//ts:source_map, //ts:declaration_map and //ts:lib_check. Each action is a
function under ts/private/actions/; compile_program declares the outputs, calls
them in order and builds the providers, for ts_compile and for the ts_test rule
over the same attributes.
"""

load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")
load(
    "//ts/private:providers.bzl",
    "NpmPackageInfo",
    "TsConfigInfo",
    "TsInfo",
    "label_text",
)
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE")
load(
    "//ts/private:toolchain.bzl",
    "OXC_TOOLCHAIN_TYPE",
    "TSGO_TOOLCHAIN_TYPE",
    "get_oxc_toolchain",
)
load("//ts/private/actions:forest.bzl", "forest_action", "forest_packages")
load("//ts/private/actions:lint.bzl", "LintConfigInfo", "lint_action")
load("//ts/private/actions:oxc.bzl", "oxc_compile_action")
load("//ts/private/actions:strict_deps.bzl", "strict_deps_check")
load(
    "//ts/private/actions:tsconfig.bzl",
    "tsconfig_action",
    "write_baseline_tsconfig",
)
load(
    "//ts/private/actions:tsgo.bzl",
    "npm_hub_entry",
    "ownership_manifest",
    "tsgo_action",
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

def compile_program(ctx):
    """Registers the actions over ctx's srcs, deps and tsconfig.

    The body of ts_compile and of ts_test: one attrs dict, one set of action
    functions. Returns struct(outputs, js, forest, packages, transitive_js,
    transitive_data, info, instrumented, output_groups).
    """
    oxc = get_oxc_toolchain(ctx)
    pkg = ctx.label.package

    compile_srcs, js_srcs, passthrough_dts, data_srcs = _classify_srcs(ctx)

    transitive_dts_sets = []
    dep_npm_package_sets = []
    transitive_js_sets = []
    transitive_js_map_sets = []
    transitive_data_sets = []

    # What the direct deps produce themselves, which is the set an import has to
    # be satisfied from. The transitive sets above stay the action inputs.
    direct_provided_sets = []

    direct_npm_infos = []
    direct_npm_names = {}
    direct_labels = []
    owner_sets = []

    # A dep linked in the forest reaches the program and the runtime there; a
    # copy of its files at their exec paths would duplicate every module.
    for dep in ctx.attr.deps:
        info = dep[TsInfo]
        dep_npm_package_sets.append(info.npm_packages)
        if NpmPackageInfo in dep:
            npm_info = dep[NpmPackageInfo]
            direct_npm_infos.append(npm_info)
            direct_npm_names[npm_info.package_name] = True
            continue
        transitive_dts_sets.append(info.transitive_declarations)
        transitive_js_sets.append(info.transitive_js)
        transitive_js_map_sets.append(info.transitive_js_maps)
        transitive_data_sets.append(info.transitive_data)
        direct_provided_sets.append(info.declarations)
        direct_provided_sets.append(info.js)
        direct_provided_sets.append(info.data)
        direct_labels.append(label_text(dep.label))
        owner_sets.append(info.owners)

    packages = forest_packages(direct_npm_infos, dep_npm_package_sets)

    # The forest links a direct package's @types twin for it (ts_npm_package's
    # types_dep), so an edge into the twin is declared by the package.
    forest_names = {info.package_name: True for info in packages}
    npm_declared = dict(direct_npm_names)
    for name in direct_npm_names:
        if not name.startswith("@") and "@types/" + name in forest_names:
            npm_declared["@types/" + name] = True
    npm_reachable = []
    for info in packages:
        if info.package_name not in npm_declared:
            npm_declared[info.package_name] = False
            npm_reachable.append(npm_hub_entry(info))

    # One entry per name for the undeclared-import check, direct deps first. A
    # workspace member has no package_dir: npm_direct answers for its view.
    reachable_by_name = {}
    for npm_info in packages:
        name = npm_info.package_name
        if npm_info.package_dir and name not in reachable_by_name:
            reachable_by_name[name] = npm_info

    dep_dts_depset = depset(
        transitive = transitive_dts_sets,
        order = "postorder",
    )

    # No deps, no closure to arrive through: nothing an import could resolve to
    # that a direct dep does not provide.
    scan_srcs = compile_srcs + js_srcs + passthrough_dts
    strict_deps = None
    if ctx.attr.deps and scan_srcs:
        strict_deps = strict_deps_check(
            ctx = ctx,
            scan_srcs = scan_srcs,
            own_files = ctx.files.srcs,
            direct_provided = depset(transitive = direct_provided_sets),
            transitive_provided = depset(transitive = (
                transitive_dts_sets + transitive_js_sets + transitive_data_sets
            )),
            npm_direct = sorted(direct_npm_names),
            npm_reachable = [
                npm_hub_entry(reachable_by_name[name])
                for name in sorted(reachable_by_name)
            ],
        )
    strict_deps_inputs = [strict_deps.stamp] if strict_deps else []
    strict_deps_gated = False

    # Starlark cannot read the file to follow its extends chain, so a ts_config
    # target declares it and every file in it is an action input.
    baseline_file = write_baseline_tsconfig(ctx)
    tsconfig_chain = [baseline_file]
    declared_jsx = ""
    if ctx.file.tsconfig:
        tsconfig_chain.append(ctx.file.tsconfig)
        if TsConfigInfo in ctx.attr.tsconfig:
            config_info = ctx.attr.tsconfig[TsConfigInfo]
            tsconfig_chain += config_info.deps_tsconfigs.to_list()
            declared_jsx = config_info.jsx
    tsx_extension = ".jsx" if declared_jsx == "preserve" else ".js"

    oxc_emits_dts = ctx.attr._declarations[BuildSettingInfo].value == "oxc"
    tsgo_emits_dts = not oxc_emits_dts
    source_map = ctx.attr._source_map[BuildSettingInfo].value
    declaration_map = ctx.attr._declaration_map[BuildSettingInfo].value

    if declaration_map and not tsgo_emits_dts:
        fail(
            "ts_compile: --//ts:declaration_map needs the tsgo declaration " +
            "emit, and --//ts:declarations=oxc takes it away.\noxc writes " +
            "declarations syntactically and emits no map for them.\nBuild " +
            "with one flag or the other.",
        )

    # One output per src at its package-relative path; oxc runs once per root.
    out_base = "/".join([
        p
        for p in [ctx.bin_dir.path, ctx.label.workspace_root, pkg]
        if p
    ])

    oxc_srcs_by_root = {}
    oxc_outs_by_root = {}
    js_outputs = []
    js_map_outputs = []
    dts_outputs = []
    dts_map_outputs = []

    for src in compile_srcs:
        stem = _package_relative_stem(src, pkg)
        root = _source_root(src, pkg)
        group_outs = oxc_outs_by_root.setdefault(root, [])
        oxc_srcs_by_root.setdefault(root, []).append(src)

        js_extension = tsx_extension if src.extension == "tsx" else ".js"
        js_out = ctx.actions.declare_file(stem + js_extension)
        js_outputs.append(js_out)
        group_outs.append(js_out)
        if source_map:
            js_map_out = ctx.actions.declare_file(stem + js_extension + ".map")
            js_map_outputs.append(js_map_out)
            group_outs.append(js_map_out)
        dts_out = ctx.actions.declare_file(stem + ".d.ts")
        dts_outputs.append(dts_out)
        if oxc_emits_dts:
            group_outs.append(dts_out)
        if declaration_map:
            dts_map_outputs.append(ctx.actions.declare_file(stem + ".d.ts.map"))

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
    for src in data_srcs:
        staged = ctx.actions.declare_file(_package_relative_path(src, pkg))
        ctx.actions.symlink(output = staged, target_file = src)
        data_staged.append(staged)

    # JavaScript srcs whose declarations are all checked in leave tsgo nothing
    # to write: a program to check, not one to emit from.
    tsgo_emits_dts = tsgo_emits_dts and bool(dts_outputs)

    all_outputs = (
        js_outputs + js_map_outputs + dts_outputs + dts_map_outputs +
        js_passthrough + data_staged
    )

    owners = depset(
        [struct(
            label = label_text(ctx.label),
            files = depset(dts_outputs + passthrough_dts + data_staged),
        )],
        transitive = owner_sets,
    )

    program_srcs = compile_srcs + js_srcs
    check_srcs = compile_srcs + js_srcs + passthrough_dts

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
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if program_srcs and not tsgo_toolchain_info:
        fail(("ts_compile: {} needs a tsgo toolchain, and none is registered." +
              "\nThe tsconfig the actions read, and the target and jsx oxc " +
              "transforms with, come from `tsgo --showConfig`.\nAdd to " +
              "MODULE.bazel:\n    register_toolchains(" +
              "\"@rules_typescript//ts/toolchain:all\")").format(ctx.label))

    tsconfig = None
    options_file = None
    if program_srcs:
        tsgo = tsgo_toolchain_info.tsgo_info

        emit = None
        if tsgo_emits_dts:
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
            emit = struct(out_dir = out_base, root_dir = root_list[0])

        written = tsconfig_action(
            ctx,
            tsgo = tsgo,
            check_srcs = check_srcs,
            tsconfig_chain = tsconfig_chain,
            baseline_file = baseline_file,
            dep_dts = dep_dts_depset,
            declared_jsx = declared_jsx,
            types_deps = sorted([
                name[len("@types/"):]
                for name in direct_npm_names
                if name.startswith("@types/")
            ]),
            emit = emit,
            declaration_map = declaration_map,
            isolated_declarations = oxc_emits_dts,
            lib_check = ctx.attr._lib_check[BuildSettingInfo].value,
        )
        tsconfig = written.tsconfig
        options_file = written.options

    for root in sorted(oxc_srcs_by_root.keys()):
        strict_deps_gated = True
        oxc_compile_action(
            ctx,
            oxc = oxc,
            srcs = oxc_srcs_by_root[root],
            outputs = oxc_outs_by_root[root],
            out_base = out_base,
            root = root,
            options_file = options_file,
            dep_dts = dep_dts_depset,
            source_map = source_map,
            emit_dts = oxc_emits_dts,
            gate = strict_deps_inputs,
        )

    forest = None
    validation_outputs = []
    if program_srcs:
        forest = forest_action(ctx, packages)
        strict_deps_gated = True
        emit_outputs = dts_outputs + dts_map_outputs if tsgo_emits_dts else []
        ownership = ownership_manifest(
            ctx,
            own = check_srcs + json_srcs,
            direct = direct_labels,
            owners = owners,
            npm_declared = sorted([
                name
                for name in npm_declared
                if npm_declared[name]
            ]),
            npm_reachable = npm_reachable,
        )
        stamp = tsgo_action(
            ctx,
            tsgo = tsgo,
            tsconfig = tsconfig,
            forest = forest,
            srcs = check_srcs + json_srcs + dep_json,
            chain = tsconfig_chain,
            gate = strict_deps_inputs,
            dep_dts = dep_dts_depset,
            ownership = ownership,
            emit_outputs = emit_outputs,
        )
        if stamp:
            validation_outputs.append(stamp)

    lint = ctx.attr._lint[LintConfigInfo]
    if lint.binary and check_srcs:
        validation_outputs.append(lint_action(ctx, lint, check_srcs))

    # A target with declarations alone has no compile action to hang the stamp
    # on, so it goes in the output group Bazel requests for every target.
    if strict_deps and not strict_deps_gated:
        validation_outputs.append(strict_deps.stamp)

    direct_dts = depset(dts_outputs + passthrough_dts, order = "postorder")
    direct_js = depset(js_outputs + js_passthrough, order = "postorder")
    direct_js_map = depset(js_map_outputs, order = "postorder")

    transitive_dts = depset(
        dts_outputs + passthrough_dts,
        transitive = transitive_dts_sets,
        order = "postorder",
    )
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

    info = TsInfo(
        js = direct_js,
        js_maps = direct_js_map,
        declarations = direct_dts,
        data = depset(data_staged, order = "postorder"),
        sources = depset(compile_srcs + passthrough_dts, order = "postorder"),
        transitive_js = transitive_js,
        transitive_js_maps = transitive_js_map,
        transitive_declarations = transitive_dts,
        transitive_data = transitive_data,
        npm_packages = depset(
            direct_npm_infos,
            transitive = dep_npm_package_sets,
            order = "postorder",
        ),
        owners = owners,
    )

    output_groups = {}

    # The tsconfig the compiler read, for a test comparing the build's
    # resolution with the editor's; a target with no program generates none.
    if tsconfig:
        output_groups["tsconfig"] = depset([tsconfig])
    if validation_outputs:
        output_groups["_validation"] = depset(validation_outputs)
    if strict_deps:
        # Requesting this group alone checks every target's deps without
        # compiling anything, since the check reads only the target's own srcs.
        output_groups["strict_deps"] = depset([
            strict_deps.stamp,
            strict_deps.checker,
        ])

    return struct(
        outputs = all_outputs + passthrough_dts,
        js = js_outputs + js_passthrough,
        forest = forest,
        packages = packages,
        transitive_js = transitive_js,
        transitive_data = transitive_data,
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
    program = compile_program(ctx)

    # This target's own outputs; a dep's reach a consumer through TsInfo.
    providers = [
        DefaultInfo(files = depset(program.outputs)),
        program.info,
        program.instrumented,
    ]
    if program.output_groups:
        providers.append(OutputGroupInfo(**program.output_groups))
    return providers

TS_COMPILE_ATTRS = {
    "srcs": attr.label_list(
        doc = """The package's files.

.ts / .tsx      compiled by oxc; one .js (+ .js.map, + .d.ts) output each. A
                .tsx under jsx: preserve emits .jsx (+ .jsx.map), the name tsc
                gives it, when the tsconfig's ts_config declares that value.
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
                a module's format and the package's own name.
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

An npm dep reaches tsgo through the node_modules forest, under its package name;
a first-party dep through its declarations, staged under bazel-bin at the paths
the tsconfig's `paths` and their bin-dir twins reach, or through a relative
import; a workspace member through the hub's view of it, `@npm//:<name>`.""",
        providers = [TsInfo],
    ),
    "tsconfig": attr.label(
        doc = """The project's own tsconfig.json: where every compiler option
comes from.

Either a .json file or a ts_config target, which additionally declares the
files the tsconfig `extends` and, with `jsx = "preserve"`, that a .tsx emits
.jsx; the rule names its outputs before any action reads the file, and the
TsConfig action fails a target with a .tsx src when the declaration and the
file disagree. The file is referenced where it lives, not copied, so relative
paths inside it keep resolving against the directory they were written for.

The action's tsconfig extends the ruleset's baseline (strict, module Preserve,
target es2022, jsx react-jsx, skipLibCheck, esModuleInterop) and then this
file, so every key the file or its own extends chain mentions wins and only the
keys it says nothing about fall back to the baseline. Over both, tsaction sets
the keys Bazel owns -- rootDirs, preserveSymlinks, the emit shape, `include` and
`files` -- rewrites `paths` to the source and bin-dir twins of each value, and
rebases each path-shaped `types` entry to the staged file it names; a `types`
entry naming a package resolves through the forest. oxc transforms with the
target, jsx and jsxImportSource tsgo reads from the same chain.

Without a tsconfig the baseline alone is the program's options.

moduleResolution the baseline never asserts: TypeScript couples it to `module`
and tsgo derives the resolver from whichever `module` wins, which is Bundler
for all of them but Node16/NodeNext.""",
        allow_single_file = [".json"],
    ),
    "_tsaction": attr.label(
        default = Label("//ts/tools/tsaction"),
        executable = True,
        cfg = "exec",
    ),
    "_lint": attr.label(
        default = Label("//ts:lint"),
        providers = [LintConfigInfo],
    ),
    "_declarations": attr.label(default = Label("//ts:declarations")),
    "_source_map": attr.label(default = Label("//ts:source_map")),
    "_declaration_map": attr.label(default = Label("//ts:declaration_map")),
    "_lib_check": attr.label(default = Label("//ts:lib_check")),
}

TS_COMPILE_TOOLCHAINS = [
    OXC_TOOLCHAIN_TYPE,
    config_common.toolchain_type(TSGO_TOOLCHAIN_TYPE, mandatory = False),
    config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
]

ts_compile = rule(
    implementation = _ts_compile_impl,
    attrs = TS_COMPILE_ATTRS,
    toolchains = TS_COMPILE_TOOLCHAINS,
    doc = """Compiles TypeScript with oxc and checks it with tsgo.

Produces one .js (+ .js.map under --//ts:source_map, the default) and one .d.ts
per .ts/.tsx input -- .jsx and .jsx.map for a .tsx under jsx: preserve, as tsc
names them -- and stages every other src -- JavaScript, JSON, anything -- into
the output tree as-is. Output paths stay relative to the target's package, so
srcs may span a subtree.

The .d.ts outputs are the compilation boundary: downstream ts_compile targets
only depend on the .d.ts files, enabling fine-grained Bazel caching.

--//ts:declarations decides who emits the .d.ts. Under "tsgo" (the default)
tsgo emits them from the full program and a type error fails the build; under
"oxc" oxc emits them syntactically, which requires an explicit type on every
export, and tsgo's check is a validation action in the _validation output group
that runs concurrently with downstream compilation. --//ts:declaration_map adds
a .d.ts.map beside each declaration under the tsgo emit; --//ts:lib_check
checks the program's .d.ts closure too.

Compiler options come from the ruleset's baseline and from `tsconfig`, read
through `tsgo --showConfig`. npm packages reach tsgo through a node_modules
forest built from `deps`.
""",
)
