"""Core TypeScript compilation rule using oxc-bazel.

ts_compile transforms .ts/.tsx source files into .js + .js.map + .d.ts outputs
using the oxc-bazel CLI as a Bazel action.

JavaScript sources (.js/.mjs/.cjs) are accepted too. They need no transform, so
they are materialised in the output tree unchanged and joined into the type
program: `import "./util.js"` resolves, JSDoc types cross the package boundary,
and `checkJs` in compiler_options type-checks them.

srcs may span a whole subtree. Every output keeps its package-relative path, so
one target can hold `index.ts` and `nested/helper.ts` together.

The .d.ts output is the compilation boundary artifact: downstream targets
depend only on .d.ts files, so Bazel's content-based caching means that if a
dep's .d.ts doesn't change (e.g. because an internal implementation detail
changed but the public API did not), dependents are not recompiled.

When a tsgo toolchain is available, ts_compile also runs type-checking as a
Bazel validation action in the _validation output group. Validation actions
run unconditionally during `bazel build` but do NOT block downstream
compilation.
"""

load("//ts/private:providers.bzl", "AssetInfo", "CssInfo", "CssModuleInfo", "JsInfo", "NpmPackageInfo", "TsConfigInfo", "TsDeclarationInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")
load("//ts/private:toolchain.bzl", "OXC_TOOLCHAIN_TYPE", "TSGO_TOOLCHAIN_TYPE", "get_oxc_toolchain")

TsModuleInfo = provider(
    doc = """The bare specifier a ts_compile target is importable as.

A first-party package imported as `@scope/pkg` rather than by relative path
needs a paths entry pointing at the .d.ts files Bazel produced for it. Only the
producing target knows where those land under the current configuration, so the
name travels with the target instead of being written into a consumer's
tsconfig by hand.
""",
    fields = {
        "module_name": "string: the bare specifier for this target, or '' if it declared none.",
        "label": "string: the label of this target, as a dep list writes it.",
        "declaration_root": "string: exec-root-relative directory the target's generated .d.ts files land in.",
        "source_root": "string: exec-root-relative package directory, where .d.ts files passed straight through stay.",
        "declared_paths": "tuple of struct(specifier, declarations): what the module's own package.json says its specifiers resolve to. `specifier` is the part after the module name -- \"\" for the bare name, \"/button\" for a subpath, \"/tokens/*\" for a wildcard one -- and `declarations` are the module-root-relative declaration paths it designates, in resolution order. Empty on a target that declared its name with `module_name`: nothing there reads a manifest.",
        "transitive_modules": "depset of struct(module_name, label, declaration_root, source_root, declared_paths): this target's modules and its deps'.",
    },
)

# ─── Helpers ──────────────────────────────────────────────────────────────────

_TS_EXTENSIONS = ["ts", "tsx"]

_JS_EXTENSIONS = ["js", "mjs", "cjs"]

# tsc's own naming for the declaration it emits from a JavaScript source.
_JS_DECLARATION_EXTENSION = {
    "js": ".d.ts",
    "mjs": ".d.mts",
    "cjs": ".d.cts",
}

def _is_dts_source(f):
    """Returns True if the file is an ambient declaration file."""
    b = f.basename
    return b.endswith(".d.ts") or b.endswith(".d.mts") or b.endswith(".d.cts")

def _package_relative_path(f, pkg):
    """Returns the path of a src relative to the target's package, extension intact."""
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
    """Returns the package-relative path with the TypeScript extension stripped."""
    return _strip_ts_extension(_package_relative_path(f, pkg))

def _source_root(f, pkg):
    """Returns the exec-root-relative directory the package-relative path hangs off.

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

def _rebased_declarations(roots, declarations):
    """Each declaration under each of the module's roots, in the order given."""
    out = []
    for root in roots:
        for declaration in declarations:
            candidate = root + "/" + declaration
            if candidate not in out:
                out.append(candidate)
    return out

def _relative_path(from_dir, to_dir):
    """Computes a relative path from from_dir to to_dir.

    Both arguments are /-separated directory paths. Returns a string like
    "../../other/pkg" or "." when from_dir == to_dir.
    """
    from_parts = [p for p in from_dir.split("/") if p]
    to_parts = [p for p in to_dir.split("/") if p]
    common_len = 0
    for i in range(min(len(from_parts), len(to_parts))):
        if from_parts[i] == to_parts[i]:
            common_len += 1
        else:
            break
    up_parts = [".."] * (len(from_parts) - common_len)
    down_parts = to_parts[common_len:]
    result = up_parts + down_parts
    return "/".join(result) if result else "."

def include_entry(tsconfig_dir, src_dir, basename):
    """Returns a src's `include` entry, relative to the generated tsconfig.

    Exported for the unit test. A src in the workspace root has an empty
    dirname, which names the exec root -- not the tsconfig's own directory.
    """
    return _relative_path(tsconfig_dir, src_dir) + "/" + basename

# ─── Tsconfig generation ─────────────────────────────────────────────────────

# Options whose values encode the sandbox layout or the action's declared
# outputs: a user value here breaks the build rather than configuring it, so
# setting one fails with the mapped text as the pointer to the real knob.
_BAZEL_OWNED_OPTIONS = {
    "baseUrl": "tsgo removed baseUrl (TS5102); use path_aliases, whose values Bazel rewrites",
    "rootDirs": "rootDirs bridges the source tree and the output tree",
    "paths": "use path_aliases for source aliases, or module_name on the target that produces the declarations",
    "outDir": "outDir must be the directory Bazel declared the outputs in",
    "rootDir": "rootDir must be the source directory oxc strips",
    "declarationDir": "declarations must land next to the .js Bazel declared",
    "declaration": "declaration emit follows from the `declarations` attr",
    "emitDeclarationOnly": "declaration emit follows from the `declarations` attr",
    "declarationMap": "declaration map emit follows from the `declaration_map` attr",
    "sourceMap": "source map emit follows from the `source_map` attr",
    "noEmit": "declaration emit follows from the `declarations` attr",
    "noEmitOnError": "a target that fails to check must not leave a declaration on disk",
    "isolatedDeclarations": "isolated declarations follow from declarations = \"oxc\"",
    "composite": "cross-target wiring is Bazel's job, not tsc's",
    "incremental": "Bazel declares no .tsbuildinfo output",
    "tsBuildInfoFile": "Bazel declares no .tsbuildinfo output",
}

# tsgo flags that report on the program without changing what it emits or how
# it resolves. Everything else is either a compilerOption -- which belongs in
# `compiler_options`, where the guard above can see it -- or a flag that would
# move outputs Bazel already declared.
_ALLOWED_TSGO_ARGS = [
    "--diagnostics",
    "--explainFiles",
    "--extendedDiagnostics",
    "--listEmittedFiles",
    "--listFiles",
    "--noErrorTruncation",
    "--traceResolution",
]

# Required by the .d.ts this ruleset generates for css_module, css_library,
# asset_library and json_library deps, whose extensions TypeScript otherwise
# refuses. Beneath the user's options, so an explicit value still wins.
_RULESET_OPTIONS = {
    "allowArbitraryExtensions": True,
}

# The options a TypeScript target gets from this ruleset whether or not it names
# a `tsconfig`. Without one they go straight into the generated config; with one
# they go into a file that config extends FIRST, so every key the user's tsconfig
# (or its own extends chain) mentions wins and only the keys it says nothing
# about fall back here. Naming a tsconfig therefore adds what the file says
# instead of subtracting what the ruleset already guaranteed.
_BASELINE_OPTIONS = {
    "strict": True,
    "module": "Preserve",
    "moduleResolution": "Bundler",
    "skipLibCheck": True,
    "esModuleInterop": True,
}

# `module` and `moduleResolution` are one setting spelled as two keys, and
# TypeScript rejects a pair it did not derive itself: Bundler under module
# NodeNext is TS5109, NodeNext under any other module is TS5110. Layers merge
# per key, so a layer whose `module` a later one replaces is left asserting the
# partner of a module that is no longer there. Defaulting `module` alone stays
# safe -- tsgo derives a legal resolver from every value it takes -- so this one
# key is asserted only by the layer that also owns `module`, and withdrawn
# everywhere else. That withdrawal changes nothing that resolves today: tsgo
# derives Bundler from every module but Node16/NodeNext, which derive their own.
_DERIVED_FROM_MODULE = "moduleResolution"

def _drop_derived_resolution(opts, overriding_layer):
    """Withdraws `moduleResolution` unless `opts` also owns the `module` it fits.

    `overriding_layer` is the options of whoever can still replace this layer's
    `module`: the user's compiler_options over the generated config, or -- with
    no way to read a tsconfig at analysis time -- None for their tsconfig over
    the baseline file, which can never be shown to have left `module` alone.
    """
    if overriding_layer == None or "module" in overriding_layer or _DERIVED_FROM_MODULE in overriding_layer:
        opts.pop(_DERIVED_FROM_MODULE, None)
    return opts

# Options whose values are paths a user copies out of a package's own
# tsconfig.json, where they are written relative to that package.
_PACKAGE_RELATIVE_OPTIONS = ["types", "typeRoots"]

def _rebase_package_relative(entry, package_rel):
    """Rewrites a package-relative path so it resolves from the generated config.

    Entries that are not relative paths (npm package names such as "node" or
    "@cloudflare/workers-types") are returned untouched.
    """
    if entry == ".":
        return package_rel
    if entry.startswith("./"):
        return package_rel + "/" + entry[2:]
    if entry.startswith("../"):
        return package_rel + "/" + entry
    return entry

# A tsconfig `types` entry names a package, and TypeScript resolves it by walking
# node_modules for that package and reading its manifest. There is no
# node_modules here -- npm packages reach the compiler through `paths`, which
# `types` does not consult -- so an entry that would have resolved natively
# resolves to nothing and its declarations never join the program.
#
# So the entry is resolved here instead, against what the package's own manifest
# designated: the root export for a bare name, and the matching `exports` subpath
# for `pkg/sub`. The file goes in `files`, which is how every other ambient
# declaration in this ruleset reaches tsgo.
def _requested_type_files(ctx, npm_info):
    requested = _requested_types(ctx)
    if not requested:
        return []
    name = npm_info.package_name
    out = []
    for entry in requested:
        if entry == name:
            if npm_info.exports_types_file:
                out.append(npm_info.exports_types_file)
        elif entry.startswith(name + "/"):
            subpath = "." + entry[len(name):]
            designated = npm_info.subpath_types.get(subpath)
            if designated:
                out.append(designated)
    return out

def _requested_types(ctx):
    """The compilerOptions.types this target asked for, or []."""
    if not ctx.attr.compiler_options_json:
        return []
    decoded = json.decode(ctx.attr.compiler_options_json)
    if type(decoded) != "dict":
        return []
    value = decoded.get("types")
    if type(value) != "list":
        return []
    return [v for v in value if type(v) == "string"]

def _write_baseline_tsconfig(ctx):
    """Writes _BASELINE_OPTIONS as a tsconfig for the generated one to extend.

    A file rather than a dict merge because Starlark cannot read the user's
    tsconfig to see which keys it already sets; TypeScript resolves that itself,
    and an `extends` list is the only place a layer can sit *under* the file.

    That same unreadability is why `moduleResolution` is withdrawn here: the
    user's `module` overrides this layer's without the layer ever learning it
    did, and the partner left behind is what tsgo rejects.
    """
    out = ctx.actions.declare_file("{}.tsconfig_baseline.json".format(ctx.label.name))
    opts = _drop_derived_resolution(dict(_BASELINE_OPTIONS), None)
    ctx.actions.write(
        output = out,
        content = json.encode_indent({"compilerOptions": opts}, indent = "  "),
    )
    return out

def _generate_tsconfig(
        ctx,
        srcs,
        npm_pkg_dirs = None,
        ambient_dts = None,
        module_paths = None,
        extends_file = None,
        baseline_file = None,
        allow_js = False,
        emit_declarations = False,
        emit_root_dir = None,
        emit_out_dir = None):
    """Generates the tsconfig.json tsgo is invoked with.

    Layered lowest precedence first:

      1. `baseline_file` -- _BASELINE_OPTIONS as a file, extended before the
         user's, so it reaches only the keys the user's chain never mentions,
         minus the one key that depends on which of the two `module` won.
      2. `extends_file` -- the user's own tsconfig.json, referenced (not
         copied), so every relative path inside it still resolves against the
         directory it was written for.
      3. _RULESET_OPTIONS and allowJs, plus _BASELINE_OPTIONS when there is no
         `extends_file` to put them under.
      4. compiler_options_json -- what the user asked for, via the ts_compile
         macro's lib / types / target / jsx / compiler_options arguments.
      5. Bazel-owned options: _BAZEL_OWNED_OPTIONS plus paths and include.

    Layers 3-5 all land in this generated file, so they override the user's
    tsconfig wholesale, per key, exactly as TypeScript's own `extends` does.

    Args:
        ctx:          Rule context.
        srcs:         Source files to type-check (.ts/.tsx/.js/.mjs/.cjs plus
                      ambient .d.ts).
        npm_pkg_dirs: (package_name, path, is_file) triples for npm deps.
        ambient_dts:  The .d.ts whose declarations are global rather than
                      imported: each @types/* dep's entry point, and each dep
                      target's global-declaration entry. Listed in `files`.
        module_paths: struct(module_name, declaration_root, source_root,
                      declared_paths) from every dep that declared a
                      module_name.
        extends_file: The user's tsconfig.json, or None for zero-config.
        baseline_file: _BASELINE_OPTIONS written out, or None when there is no
                      `extends_file` to layer them under.
        allow_js:     True when a JavaScript src is in `include`.
        emit_declarations: True when tsgo emits the .d.ts (declarations =
                      "tsgo"), False when it only reports diagnostics.
        emit_root_dir: Exec-root-relative common source directory.
        emit_out_dir: Exec-root-relative directory the .d.ts must land in.
    """
    tsconfig = ctx.actions.declare_file("{}.tsconfig.json".format(ctx.label.name))
    tsconfig_dir = tsconfig.dirname
    package_rel = _relative_path(tsconfig_dir, ctx.label.package)

    opts = {}
    opts.update(_RULESET_OPTIONS)

    # A JavaScript src is listed in `include`; without this tsgo reports TS6504
    # on it rather than reading it.
    if allow_js:
        opts["allowJs"] = True

    user_opts = {}
    if ctx.attr.compiler_options_json:
        decoded = json.decode(ctx.attr.compiler_options_json)
        if type(decoded) != "dict":
            fail("ts_compile: compiler_options must be a dict, got {}.".format(type(decoded)))
        user_opts = decoded

    # With an extends_file the same options arrive through baseline_file, under
    # it rather than over it. Decoded first because here -- unlike there -- the
    # layer that can override the baseline's `module` is readable, so the
    # baseline keeps `moduleResolution` for as long as it still owns the module
    # the value belongs to.
    if not extends_file:
        opts.update(_drop_derived_resolution(dict(_BASELINE_OPTIONS), user_opts))

    # target and jsx come from the attrs in every mode, including over a
    # tsconfig baseline: oxc transforms with them, and the two compilers
    # disagreeing is worse than deferring to the file.
    opts["target"] = ctx.attr.target
    if ctx.attr.jsx_mode:
        opts["jsx"] = ctx.attr.jsx_mode

    # The resolvers with a `module` of their own: withdrawing the baseline's
    # `module` would not help, because the value tsgo derives for an unset
    # `module` is not one of them either. So this pair is named here instead of
    # at line 2 of a generated file. Only without a `tsconfig`, whose `module`
    # could be the matching one.
    resolution = user_opts.get(_DERIVED_FROM_MODULE)
    if not extends_file and "module" not in user_opts and type(resolution) == "string" and resolution.lower() in ("node16", "nodenext"):
        fail(
            "ts_compile: compilerOptions.moduleResolution is \"{}\" and no module is set, so it inherits\n".format(resolution) +
            "the ruleset's module \"{}\" -- a pair TypeScript rejects (TS5110).\n".format(_BASELINE_OPTIONS["module"]) +
            "Set compilerOptions.module to \"{}\" on {}, or name a tsconfig that does.".format(resolution, ctx.label),
        )

    for key, reason in _BAZEL_OWNED_OPTIONS.items():
        if key in user_opts:
            fail(
                "ts_compile: compilerOptions.{key} is set by the rule and cannot be overridden -- {reason}.\n".format(
                    key = key,
                    reason = reason,
                ) +
                "Remove \"{}\" from compiler_options on {}.".format(key, ctx.label),
            )

    for key in _PACKAGE_RELATIVE_OPTIONS:
        if key in user_opts:
            if type(user_opts[key]) != "list":
                fail("ts_compile: compilerOptions.{} must be a list of strings on {}.".format(key, ctx.label))
            user_opts[key] = [
                _rebase_package_relative(entry, package_rel)
                for entry in user_opts[key]
            ]

    opts.update(user_opts)

    # ── Bazel-owned: module resolution ────────────────────────────────────
    #
    # paths is one key, so it cannot be half-inherited: everything importable
    # has to be represented here.
    paths = {}

    for alias_key, alias_dir in ctx.attr.path_aliases.items():
        dir_no_slash = alias_dir[:-1] if alias_dir.endswith("/") else alias_dir
        rel_dir = _relative_path(tsconfig_dir, dir_no_slash)
        if alias_key.endswith("/"):
            paths[alias_key + "*"] = [rel_dir + "/*"]
            paths[alias_key[:-1]] = [rel_dir]
        else:
            paths[alias_key] = [rel_dir]
            paths[alias_key + "/*"] = [rel_dir + "/*"]

    for entry in npm_pkg_dirs or []:
        pkg_name, path, is_file = entry[0], entry[1], entry[2]
        pkg_dir = path[:path.rfind("/")] if "/" in path else ""
        rel_dir = _relative_path(tsconfig_dir, pkg_dir if is_file else path)
        if is_file:
            paths[pkg_name] = [rel_dir + "/" + path.split("/")[-1]]
        else:
            paths[pkg_name] = [rel_dir]
        paths[pkg_name + "/*"] = [rel_dir + "/*"]

    # Last, so a first-party module_name wins over a same-named npm package.
    # Both roots are listed because a module's declarations are either generated
    # or passed through from srcs; TypeScript tries each entry in turn, and the
    # generated root goes first because it is what this build produced.
    #
    # What the module's own package.json designates goes ahead of the guesses
    # that used to be the whole answer, and the guesses stay: a manifest naming a
    # file this build does not produce is then no worse than a manifest nobody
    # read. That is also why a declared subpath repeats the wildcard expansion --
    # an exact `paths` key beats a pattern one, so `<name>/*` stops being
    # consulted for a subpath the moment it is named.
    for module in module_paths or []:
        roots = [
            _relative_path(tsconfig_dir, module.declaration_root),
            _relative_path(tsconfig_dir, module.source_root),
        ]
        declared = {d.specifier: d.declarations for d in module.declared_paths}
        paths[module.module_name] = (
            _rebased_declarations(roots, declared.get("", ())) +
            [r + "/index.d.ts" for r in roots] + roots
        )
        paths[module.module_name + "/*"] = (
            _rebased_declarations(roots, declared.get("/*", ())) +
            [r + "/*" for r in roots]
        )
        for specifier in sorted(declared):
            if specifier == "" or specifier == "/*":
                continue
            paths[module.module_name + specifier] = (
                _rebased_declarations(roots, declared[specifier]) +
                [r + specifier for r in roots]
            )

    if paths:
        opts["paths"] = paths

    opts["rootDirs"] = [
        _relative_path(tsconfig_dir, ""),
        _relative_path(tsconfig_dir, ctx.bin_dir.path),
    ]

    # ── Bazel-owned: emit shape ───────────────────────────────────────────
    opts["declaration"] = True
    opts["emitDeclarationOnly"] = True
    opts["declarationMap"] = ctx.attr.declaration_map
    opts["composite"] = False
    opts["incremental"] = False

    if emit_declarations:
        # noEmit off because a tsconfig that sets it -- the usual shape for a
        # bundler-built package -- would starve the action of its declared
        # outputs; noEmitOnError so a target that fails to check leaves no
        # declaration behind for a consumer to read.
        opts["noEmit"] = False
        opts["noEmitOnError"] = True

        # Compared against None, not tested for truth: "" is the exec root, the
        # source root of a target in the top-level package, and not "unset".
        opts["outDir"] = _relative_path(tsconfig_dir, emit_out_dir) if emit_out_dir != None else "."
        opts["declarationDir"] = opts["outDir"]
        opts["rootDir"] = _relative_path(tsconfig_dir, emit_root_dir) if emit_root_dir != None else "."
    else:
        # oxc's syntactic emit genuinely requires isolated declarations. In tsgo
        # mode the compiler has the full program and infers the types, so
        # demanding annotations would buy nothing.
        opts["isolatedDeclarations"] = True

    # ── Bazel-owned: the file set ─────────────────────────────────────────

    include = []
    for src in srcs:
        include.append(include_entry(tsconfig_dir, src.dirname, src.basename))

    # Globals are declared, never imported, so `include` -- which only ever names
    # this target's own srcs -- would leave every one of them out of the program.
    files = [
        include_entry(tsconfig_dir, f.dirname, f.basename)
        for f in ambient_dts or []
    ]

    config = {}
    if extends_file:
        extends_dir = _relative_path(tsconfig_dir, extends_file.dirname)

        # TypeScript reads an `extends` that is not visibly relative as a node
        # module specifier.
        if not extends_dir.startswith("."):
            extends_dir = "./" + extends_dir
        chain = [extends_dir + "/" + extends_file.basename]

        # A list, and the ruleset's baseline first: a later entry overrides an
        # earlier one, so the user's file and its own extends chain win every key
        # they mention and the baseline reaches only the rest.
        if baseline_file:
            chain.insert(0, "./" + baseline_file.basename)
        config["extends"] = chain if len(chain) > 1 else chain[0]
    config["compilerOptions"] = opts
    config["include"] = include

    if extends_file:
        # srcs and the ambient packages are the only file list. Emptying the
        # inherited ones is safe only alongside `extends`: TS18002 rejects an
        # empty `files` otherwise.
        config["files"] = files
        config["exclude"] = []
        config["references"] = []
    elif files:
        config["files"] = files

    ctx.actions.write(output = tsconfig, content = json.encode_indent(config, indent = "  "))
    return tsconfig

# ─── Undeclared imports ──────────────────────────────────────────────────────
#
# An import has to be satisfied by a DIRECT dep. Inputs stay transitive and
# resolution becomes direct, and the split has to happen here rather than in the
# tsconfig: one `paths` map serves the whole program, so dropping the transitive
# entries would also stop a declared dep's own .d.ts from resolving ITS imports,
# which widens those types to `any` instead of reporting anything.
#
# So the check reads the target's own sources and asks, per specifier, whether a
# direct dep provides it. Only Bazel can answer that, and only Bazel knows the
# label to name in the answer, which is what the compiler's own "cannot find
# module" cannot tell anyone.
#
# The action's inputs are the target's own srcs plus a manifest of what the deps
# provide, so it never waits on an upstream compile.

# Embedded rather than a checked-in .mjs so that the manifest format and its one
# reader stay in the same file. Escape sequences are doubled: this is a Starlark
# string literal.
_STRICT_DEPS_MJS = """\
import { readFileSync, writeFileSync } from "node:fs";
import { builtinModules } from "node:module";

const MODULE_EXTENSIONS = [
  ".d.ts", ".d.mts", ".d.cts",
  ".tsx", ".ts", ".mts", ".cts",
  ".jsx", ".js", ".mjs", ".cjs",
];

const cfg = {
  label: "",
  bin: "",
  aliases: [],
  scan: [],
  own: new Set(),
  direct: new Set(),
  transitive: new Map(),
  npmDirect: new Set(),
  npmTransitive: new Map(),
  moduleDirect: [],
  moduleTransitive: new Map(),
};

const builtins = new Set(builtinModules);

function stripBin(p) {
  return cfg.bin && p.startsWith(cfg.bin + "/") ? p.slice(cfg.bin.length + 1) : p;
}

function stripExtension(p) {
  for (const ext of MODULE_EXTENSIONS) {
    if (p.endsWith(ext)) return p.slice(0, -ext.length);
  }
  return p;
}

function keysOf(execPath) {
  const raw = stripBin(execPath);
  const bare = stripExtension(raw);
  return raw === bare ? [raw] : [raw, bare];
}

function normalize(p) {
  const out = [];
  for (const part of p.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === ".." && out.length > 0 && out[out.length - 1] !== "..") {
      out.pop();
      continue;
    }
    out.push(part);
  }
  return out.join("/");
}

function packageOf(specifier) {
  if (specifier.startsWith("@")) {
    const parts = specifier.slice(1).split("/");
    return parts.length >= 2 ? "@" + parts[0] + "/" + parts[1] : specifier;
  }
  return specifier.split("/")[0];
}

function underPrefix(specifier, prefix) {
  if (specifier === prefix) return true;
  return specifier.startsWith(prefix.endsWith("/") ? prefix : prefix + "/");
}

// ── The specifier scanner ────────────────────────────────────────────────────
//
// A character walk rather than a regex: a quoted string is an import specifier
// only when the tokens before it say so, which is what keeps `{ from: "x" }`
// and `declare module "x"` out of the results.
//
// Gazelle's ScanImports (gazelle/imports.go) is this same walk: a specifier
// only one of them sees is either a dep Gazelle cannot generate or drift the
// build never notices, so //tests/strict_deps pins the two against one table.
const KEYWORDS_BEFORE_REGEX = new Set([
  "return", "typeof", "instanceof", "in", "of", "new", "delete", "void",
  "do", "else", "yield", "await", "case", "throw",
]);

const CLOSERS = ")]";

function specifiersIn(source) {
  const found = [];
  let line = 1;
  let lastWord = "";
  let lastKind = "";
  let lastPunct = "";
  const i = { at: 0 };

  const isWordChar = (c) => /[A-Za-z0-9_$]/.test(c);

  while (i.at < source.length) {
    const c = source[i.at];

    if (c === "\\n") {
      line += 1;
      i.at += 1;
      continue;
    }
    if (c === " " || c === "\\t" || c === "\\r") {
      i.at += 1;
      continue;
    }
    if (c === "/" && source[i.at + 1] === "/") {
      while (i.at < source.length && source[i.at] !== "\\n") i.at += 1;
      continue;
    }
    if (c === "/" && source[i.at + 1] === "*") {
      i.at += 2;
      while (i.at < source.length && !(source[i.at] === "*" && source[i.at + 1] === "/")) {
        if (source[i.at] === "\\n") line += 1;
        i.at += 1;
      }
      i.at += 2;
      continue;
    }
    if (c === "/" && ((lastKind === "punct" && !CLOSERS.includes(lastPunct)) || (lastKind === "word" && KEYWORDS_BEFORE_REGEX.has(lastWord)))) {
      // A regex literal. Its body can hold quotes and slashes, so it has to be
      // skipped whole rather than tokenized.
      i.at += 1;
      let inClass = false;
      while (i.at < source.length) {
        const r = source[i.at];
        if (r === "\\\\") { i.at += 2; continue; }
        if (r === "\\n") break;
        if (r === "[") inClass = true;
        else if (r === "]") inClass = false;
        else if (r === "/" && !inClass) { i.at += 1; break; }
        i.at += 1;
      }
      lastKind = "punct";
      continue;
    }
    if (c === "`") {
      i.at += 1;
      while (i.at < source.length && source[i.at] !== "`") {
        if (source[i.at] === "\\\\") { i.at += 2; continue; }
        if (source[i.at] === "\\n") line += 1;
        i.at += 1;
      }
      i.at += 1;
      lastKind = "string";
      continue;
    }
    if (c === '"' || c === "'") {
      const startLine = line;
      const quote = c;
      let value = "";
      i.at += 1;
      while (i.at < source.length && source[i.at] !== quote) {
        if (source[i.at] === "\\\\") {
          value += source[i.at + 1] ?? "";
          i.at += 2;
          continue;
        }
        if (source[i.at] === "\\n") break;
        value += source[i.at];
        i.at += 1;
      }
      i.at += 1;
      const isSpecifier =
        (lastKind === "word" && (lastWord === "from" || lastWord === "import")) ||
        (lastKind === "call" && (lastWord === "import" || lastWord === "require"));
      if (isSpecifier && value !== "") found.push({ specifier: value, line: startLine });
      lastKind = "string";
      continue;
    }
    if (isWordChar(c)) {
      let word = "";
      while (i.at < source.length && isWordChar(source[i.at])) {
        word += source[i.at];
        i.at += 1;
      }
      lastWord = word;
      lastKind = "word";
      continue;
    }
    if (c === "(" && lastKind === "word") {
      lastKind = "call";
      i.at += 1;
      continue;
    }
    lastPunct = c;
    lastKind = "punct";
    i.at += 1;
  }

  return found;
}

// ── Manifest ─────────────────────────────────────────────────────────────────

const [stampPath, ...rest] = process.argv.slice(2);
const manifest = [];
for (const arg of rest) {
  if (arg.startsWith("@")) manifest.push(...readFileSync(arg.slice(1), "utf8").split("\\n"));
  else manifest.push(arg);
}

for (const entry of manifest) {
  if (entry === "") continue;
  const field = entry.split("\\t");
  switch (field[0]) {
    case "label": cfg.label = field[1]; break;
    case "bin": cfg.bin = field[1]; break;
    case "alias": cfg.aliases.push(field[1]); break;
    case "scan": cfg.scan.push(field[1]); break;
    case "own": for (const k of keysOf(field[1])) cfg.own.add(k); break;
    case "direct": for (const k of keysOf(field[1])) cfg.direct.add(k); break;
    case "transitive":
      for (const k of keysOf(field[1])) {
        if (!cfg.transitive.has(k)) cfg.transitive.set(k, field[2]);
      }
      break;
    case "npm-direct": cfg.npmDirect.add(field[1]); break;
    case "npm-transitive": cfg.npmTransitive.set(field[1], field[2]); break;
    case "module-direct": cfg.moduleDirect.push(field[1]); break;
    case "module-transitive": cfg.moduleTransitive.set(field[1], field[2]); break;
  }
}

// ── Classification ───────────────────────────────────────────────────────────

function undeclared(specifier, file) {
  const clean = specifier.split("?")[0].split("#")[0];
  if (clean === "") return null;

  if (clean.startsWith("./") || clean.startsWith("../")) {
    const dir = stripBin(file).split("/").slice(0, -1).join("/");
    const resolved = normalize(dir + "/" + clean);
    const candidates = [resolved, stripExtension(resolved)];
    for (const c of [...candidates]) candidates.push(c + "/index");
    for (const c of candidates) {
      if (cfg.own.has(c) || cfg.direct.has(c)) return null;
    }
    for (const c of candidates) {
      if (cfg.transitive.has(c)) return cfg.transitive.get(c);
    }
    return null;
  }

  if (clean.startsWith("node:")) return null;
  for (const alias of cfg.aliases) {
    if (underPrefix(clean, alias)) return null;
  }
  const pkg = packageOf(clean);
  if (builtins.has(pkg) || cfg.npmDirect.has(pkg)) return null;
  for (const name of cfg.moduleDirect) {
    if (underPrefix(clean, name)) return null;
  }
  if (cfg.npmTransitive.has(pkg)) return cfg.npmTransitive.get(pkg);
  for (const [name, label] of cfg.moduleTransitive) {
    if (underPrefix(clean, name)) return label;
  }
  return null;
}

const findings = [];
for (const file of cfg.scan) {
  let source;
  try {
    source = readFileSync(file, "utf8");
  } catch {
    continue;
  }
  for (const { specifier, line } of specifiersIn(source)) {
    const label = undeclared(specifier, file);
    if (label !== null) findings.push({ file: stripBin(file), line, specifier, label });
  }
}

if (findings.length === 0) {
  writeFileSync(stampPath, "");
  process.exit(0);
}

const width = Math.max(...findings.map((f) => `${f.file}:${f.line}`.length));
const lines = [
  `${cfg.label} imports ${findings.length === 1 ? "a module" : "modules"} no direct dep provides:`,
  "",
];
for (const f of findings) {
  lines.push(`  ${`${f.file}:${f.line}`.padEnd(width)}  imports ${JSON.stringify(f.specifier)}`);
  lines.push(`  ${" ".repeat(width)}  add ${JSON.stringify(f.label)} to deps`);
}
lines.push(
  "",
  "Each of those resolves today only because it reaches this target through",
  "another dep's own deps, and stops resolving the moment that dep drops it.",
  "Re-run gazelle to regenerate deps, or add the labels above by hand.",
);
process.stderr.write(lines.join("\\n") + "\\n");
process.exit(1);
"""

def _label_text(label):
    """The label as a deps list writes it: the main repo's canonical @@ dropped."""
    text = str(label)
    return text[2:] if text.startswith("@@//") else text

def _npm_hub_entry(npm_info):
    """The hub label a deps list writes for an npm package in the closure.

    The closure carries NpmPackageInfo, not labels: a transitive package was
    never named in any deps list here. Its own repository is
    `<hub>__<package>__<version>...`, so the hub the extension created -- which
    is what a deps list names -- is recoverable from the file it provides.
    """
    name = npm_info.package_name
    label_name = name[1:].replace("/", "_") if name.startswith("@") else name
    hub = "npm"
    owner = npm_info.package_dir.owner if npm_info.package_dir else None
    if owner and owner.repo_name:
        candidate = owner.repo_name.split("__")[0].split("+")[-1]
        if candidate:
            hub = candidate
    return struct(name = name, label = "@{}//:{}".format(hub, label_name))

def _direct_manifest_entry(f):
    return "direct\t" + f.path

def _transitive_manifest_entry(f):
    owner = f.owner

    # An external-repo file is an npm package's, and no relative specifier in a
    # first-party source reaches one; the npm entries below carry those by name.
    if not owner or owner.repo_name:
        return None
    return "transitive\t{}\t{}".format(f.path, _label_text(owner))

def _strict_deps_check(
        ctx,
        scan_srcs,
        own_files,
        direct_provided,
        transitive_provided,
        npm_direct,
        npm_reachable,
        module_direct,
        module_reachable):
    """Registers the action that fails on an import no direct dep provides.

    Args:
        ctx:                 Rule context.
        scan_srcs:           This target's own sources, the files to read.
        own_files:           Files this target already stages: srcs and
                             path_alias_srcs.
        direct_provided:     depset of File: what the direct deps produce.
        transitive_provided: depset of File: the whole closure, for the label
                             an undeclared import has to be attributed to.
        npm_direct:          npm package names of the direct deps.
        npm_reachable:       struct(name, label) per npm package in the closure.
        module_direct:       module_name of each direct dep that set one.
        module_reachable:    struct(module_name, label, ...) for the closure.

    Returns:
        struct(stamp, checker): the stamp the compile actions take as an input,
        and the checker that wrote it.
    """
    js_tool = get_js_tool(ctx)
    if not js_tool:
        fail(
            "ts_compile: checking {} for undeclared imports needs a JS tool ".format(ctx.label) +
            "toolchain, and none is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    checker = ctx.actions.declare_file("{}.strictdeps.mjs".format(ctx.label.name))
    ctx.actions.write(output = checker, content = _STRICT_DEPS_MJS)
    stamp = ctx.actions.declare_file("{}.strictdeps".format(ctx.label.name))

    # A params file, so the closure is expanded when the action runs rather than
    # materialised at analysis time.
    manifest = ctx.actions.args()
    manifest.use_param_file("@%s", use_always = True)
    manifest.set_param_file_format("multiline")

    # The scalars first: the reader keys paths off bin_dir as it parses.
    manifest.add("label\t" + _label_text(ctx.label))
    manifest.add("bin\t" + ctx.bin_dir.path)
    for alias in ctx.attr.path_aliases:
        manifest.add("alias\t" + alias)
    for name in npm_direct:
        manifest.add("npm-direct\t" + name)
    for pkg in npm_reachable:
        if pkg.name not in npm_direct:
            manifest.add("npm-transitive\t{}\t{}".format(pkg.name, pkg.label))
    for name in module_direct:
        manifest.add("module-direct\t" + name)
    for module in module_reachable:
        if module.module_name:
            manifest.add("module-transitive\t{}\t{}".format(module.module_name, module.label))

    manifest.add_all(scan_srcs, format_each = "scan\t%s")
    manifest.add_all(own_files, format_each = "own\t%s")
    manifest.add_all(direct_provided, map_each = _direct_manifest_entry)
    manifest.add_all(transitive_provided, map_each = _transitive_manifest_entry)

    ctx.actions.run(
        inputs = depset(scan_srcs + [checker]),
        outputs = [stamp],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [checker.path, stamp.path, manifest],
        mnemonic = "TsStrictDeps",
        progress_message = "TsStrictDeps %{label}",
    )
    return struct(stamp = stamp, checker = checker)

# ─── Global declarations ─────────────────────────────────────────────────────
#
# A .d.ts with no top-level import or export declares globals, and a global
# belongs to every program the file is part of. `include` names only a target's
# own srcs, so a dep's global .d.ts has no route into a consumer's program;
# `files` is that route, the one an @types/* package's globals already take.
#
# Which srcs are global cannot be decided here, because Starlark cannot read a
# file. So an action decides, and writes its answer as a generated .d.ts of
# references to the global ones: a file whose contents are known only after it
# runs, but whose path a consumer's `files` can name at analysis time.

_GLOBAL_DTS_MJS = """\
import { readFileSync, writeFileSync } from "node:fs";

function* tokens(source) {
  const isWordChar = (c) => /[A-Za-z0-9_$]/.test(c);
  let i = 0;
  while (i < source.length) {
    const c = source[i];
    if (c === "/" && source[i + 1] === "/") {
      while (i < source.length && source[i] !== "\\n") i += 1;
      continue;
    }
    if (c === "/" && source[i + 1] === "*") {
      i += 2;
      while (i < source.length && !(source[i] === "*" && source[i + 1] === "/")) i += 1;
      i += 2;
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      i += 1;
      while (i < source.length && source[i] !== c) i += source[i] === "\\\\" ? 2 : 1;
      i += 1;
      yield { kind: "string" };
      continue;
    }
    if (isWordChar(c)) {
      let word = "";
      while (i < source.length && isWordChar(source[i])) {
        word += source[i];
        i += 1;
      }
      yield { kind: "word", value: word };
      continue;
    }
    i += 1;
    if (c !== " " && c !== "\\t" && c !== "\\r" && c !== "\\n") yield { kind: "punct", value: c };
  }
}

// TypeScript's own test: a top-level import or export declaration makes the
// file a module, and everything it declares is scoped to it.
function isModule(source) {
  let depth = 0;
  let pending = "";
  let prev = null;
  for (const token of tokens(source)) {
    // `import(...)` is a type query, `import.meta` an expression, and
    // `export as namespace X` declares a UMD global rather than an export.
    if (pending === "import" && !(token.kind === "punct" && (token.value === "(" || token.value === "."))) return true;
    if (pending === "export" && !(token.kind === "word" && token.value === "as")) return true;
    pending = "";
    if (depth === 0 &&
        token.kind === "word" &&
        (token.value === "import" || token.value === "export") &&
        !(prev && prev.kind === "punct" && prev.value === ".")) {
      pending = token.value;
    }
    if (token.kind === "punct") {
      if (token.value === "{" || token.value === "(" || token.value === "[") depth += 1;
      if (token.value === "}" || token.value === ")" || token.value === "]") depth -= 1;
    }
    prev = token;
  }
  return pending !== "";
}

const [entryPath, ...rest] = process.argv.slice(2);
const manifest = [];
for (const arg of rest) {
  if (arg.startsWith("@")) manifest.push(...readFileSync(arg.slice(1), "utf8").split("\\n"));
  else manifest.push(arg);
}

const references = [];
for (const entry of manifest) {
  if (entry === "") continue;
  const field = entry.split("\\t");
  if (isModule(readFileSync(field[0], "utf8"))) continue;
  references.push('/// <reference path="' + field[1] + '" />');
}
writeFileSync(entryPath, references.join("\\n") + "\\n");
"""

def _global_dts_entry(ctx, dts_srcs):
    """Registers the action that writes this target's global-declaration entry.

    Args:
        ctx:      Rule context.
        dts_srcs: The .d.ts files in srcs, module-scoped ones included.

    Returns:
        File: a generated .d.ts referencing the global ones among them.
    """
    js_tool = get_js_tool(ctx)
    if not js_tool:
        fail(
            "ts_compile: telling a global .d.ts in {} from a module-scoped one ".format(ctx.label) +
            "needs a JS tool toolchain, and none is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    scanner = ctx.actions.declare_file("{}.globals.mjs".format(ctx.label.name))
    ctx.actions.write(output = scanner, content = _GLOBAL_DTS_MJS)
    entry = ctx.actions.declare_file("{}.globals.d.ts".format(ctx.label.name))

    manifest = ctx.actions.args()
    manifest.use_param_file("@%s", use_always = True)
    manifest.set_param_file_format("multiline")
    for f in dts_srcs:
        manifest.add("{}\t{}".format(
            f.path,
            _relative_path(entry.dirname, f.dirname) + "/" + f.basename,
        ))

    ctx.actions.run(
        inputs = depset(dts_srcs + [scanner]),
        outputs = [entry],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [scanner.path, entry.path, manifest],
        mnemonic = "TsGlobalDts",
        progress_message = "TsGlobalDts %{label}",
    )
    return entry

# ─── Attribute validation ────────────────────────────────────────────────────

def _classify_srcs(ctx):
    """Splits srcs into the TypeScript, JavaScript and ambient-declaration sets."""
    compile_srcs = []
    js_srcs = []
    passthrough_dts = []
    for f in ctx.files.srcs:
        if _is_dts_source(f):
            passthrough_dts.append(f)
        elif f.extension in _TS_EXTENSIONS:
            compile_srcs.append(f)
        elif f.extension in _JS_EXTENSIONS:
            js_srcs.append(f)
        elif f.extension == "jsx":
            fail(
                "ts_compile: '{}' on {} is a .jsx file, which oxc has no ".format(f.short_path, ctx.label) +
                "output extension for.\nRename it to .tsx -- TypeScript accepts the " +
                "JavaScript in it unchanged -- or drop the JSX and call it .js.",
            )
        else:
            fail(
                "ts_compile: srcs must contain only .ts, .tsx, .js, .mjs, .cjs " +
                "or .d.ts files; got '{}' (extension: .{}).\n".format(f.short_path, f.extension) +
                "Remove this file from srcs, or if you need to pass through assets " +
                "use a filegroup or a dedicated rule for that file type.\n" +
                "Did you mean to add it to a different attribute?",
            )
    return compile_srcs, js_srcs, passthrough_dts

def _validate_tsgo_args(ctx):
    """Rejects a tsgo flag that would move an output or change resolution."""
    for arg in ctx.attr.tsgo_args:
        if arg not in _ALLOWED_TSGO_ARGS:
            fail(
                "ts_compile: tsgo_args on {} contains \"{}\".\n".format(ctx.label, arg) +
                "Only flags that report on the program are allowed:\n  " +
                " ".join(_ALLOWED_TSGO_ARGS) + "\n" +
                "A compilerOption belongs in compiler_options (or in the file `tsconfig` " +
                "points at), where the Bazel-owned-key guard can see it. Anything else " +
                "would move outputs this rule already declared to Bazel.",
            )

def _validate_path_aliases(ctx, srcs):
    """Rejects an alias that points into bazel-out, or at files no action stages.

    An alias is a source-level path, so its target directory has to be staged by
    the same action that resolves it. Only srcs and path_alias_srcs are inputs,
    so an alias covered by neither resolves against whatever the sandbox happens
    to hold -- which is what makes the build order-dependent.
    """
    covered = [f.short_path for f in srcs] + [
        f.short_path
        for f in ctx.files.path_alias_srcs
    ]
    for alias_key, alias_dir in ctx.attr.path_aliases.items():
        if alias_dir.startswith("bazel-out/") or alias_dir.startswith("bazel-bin/"):
            fail(
                "ts_compile: path_aliases[\"{}\"] on {} points into the output tree ({}).\n".format(
                    alias_key,
                    ctx.label,
                    alias_dir,
                ) +
                "That path embeds the build configuration, so it breaks under -c opt or a " +
                "different exec platform.\nTo import another package by bare specifier, set " +
                "module_name on the target that produces its declarations and depend on it.",
            )
        prefix = alias_dir if alias_dir.endswith("/") else alias_dir + "/"
        found = False
        for path in covered:
            if path == alias_dir or path.startswith(prefix):
                found = True
                break
        if not found:
            fail(
                "ts_compile: path_aliases[\"{}\"] on {} points at \"{}\", where none of ".format(
                    alias_key,
                    ctx.label,
                    alias_dir,
                ) +
                "this target's inputs live.\nThe type-check action stages srcs and " +
                "path_alias_srcs and nothing else, so the alias would resolve against " +
                "whatever another action happened to leave in the sandbox.\nList the " +
                "files it resolves to in path_alias_srcs, or set module_name on the " +
                "target that produces them and depend on it instead.",
            )

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_compile_impl(ctx):
    oxc = get_oxc_toolchain(ctx)
    pkg = ctx.label.package

    compile_srcs, js_srcs, passthrough_dts = _classify_srcs(ctx)
    _validate_tsgo_args(ctx)
    _validate_path_aliases(ctx, compile_srcs + js_srcs + passthrough_dts)

    # Collect transitive deps.
    transitive_dts_sets = []
    global_entry_sets = []
    transitive_js_sets = []
    transitive_js_map_sets = []
    transitive_css_sets = []
    transitive_css_module_sets = []
    transitive_css_exports_sets = []
    transitive_asset_sets = []

    # What the direct deps produce themselves, which is the set an import has to
    # be satisfied from. The transitive sets above stay the action inputs.
    direct_provided_sets = []

    # npm_pkg_dirs: list of (package_name, package_dir_path) for tsconfig paths.
    # We collect ALL transitive npm deps so that tsgo can resolve bare module
    # specifiers in transitively-imported .d.ts files (e.g. vitest's index.d.ts
    # imports from @vitest/runner which must be in the tsconfig paths).
    #
    # Pass 1: collect ALL transitive package dirs from all direct npm deps.
    # This builds a complete map of pkg_name → dir_path that covers both direct
    # and transitive packages. Materializing transitive_deps is O(transitive npm
    # packages) which is bounded (typically tens to low hundreds of packages).
    #
    # We separate this into two passes:
    #  Pass 1: collect all transitive package infos (name → dir_path).
    #  Pass 2: for @types/* packages, find which runtime package they type-annotate
    #           and override the runtime package's dir with the @types dir.
    # This avoids the bug where transitive @types entries from unrelated deps
    # pollute the mapping (e.g. vitest's transitive @types/estree dep being used
    # for vitest).
    #
    # ambient_dts: entry-point .d.ts of each @types/* package in `deps`, keyed by
    # path to dedupe. _generate_tsconfig lists them in `files` so the globals and
    # `declare module` blocks they hold join the program.
    ambient_dts = {}

    # transitive_package_dir_sets: depset of package.json Files from all
    # direct npm deps, used as inputs to the tsgo validation action so that
    # moduleResolution:"Bundler" can read exports/types fields.
    transitive_package_dir_sets = []

    # Step 1a: collect ALL package info entries (direct + transitive) into a map.
    # pkg_info_map: pkg_name → NpmPackageInfo (first seen wins for dedup).
    pkg_info_map = {}
    direct_npm_names = {}

    for dep in ctx.attr.deps:
        if TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            global_entry_sets.append(dep[TsDeclarationInfo].transitive_global_entry_files)
            direct_provided_sets.append(dep[TsDeclarationInfo].declaration_files)

            # Direct deps only: declaring @types/foo is how a target asks for
            # foo's globals, so a package that merely appears in some dep's own
            # closure does not silently put its globals in this target's scope.
            if NpmPackageInfo in dep and dep[NpmPackageInfo].ambient_types_file:
                entry = dep[NpmPackageInfo].ambient_types_file
                ambient_dts[entry.path] = entry
            if NpmPackageInfo in dep:
                for entry in _requested_type_files(ctx, dep[NpmPackageInfo]):
                    ambient_dts[entry.path] = entry
        if JsInfo in dep:
            transitive_js_sets.append(dep[JsInfo].transitive_js_files)
            transitive_js_map_sets.append(dep[JsInfo].transitive_js_map_files)
            direct_provided_sets.append(dep[JsInfo].js_files)
        if CssInfo in dep:
            transitive_css_sets.append(dep[CssInfo].transitive_css_files)
            direct_provided_sets.append(dep[CssInfo].css_files)
        if CssModuleInfo in dep:
            transitive_css_module_sets.append(dep[CssModuleInfo].transitive_css_files)
            transitive_css_exports_sets.append(dep[CssModuleInfo].transitive_exports_files)
            direct_provided_sets.append(dep[CssModuleInfo].css_files)
        if AssetInfo in dep:
            transitive_asset_sets.append(dep[AssetInfo].transitive_asset_files)
            direct_provided_sets.append(dep[AssetInfo].asset_files)
        if NpmPackageInfo in dep:
            npm_info = dep[NpmPackageInfo]

            # Add the direct dep itself.
            pkg_name = npm_info.package_name
            direct_npm_names[pkg_name] = True
            if pkg_name not in pkg_info_map and npm_info.package_dir:
                pkg_info_map[pkg_name] = npm_info

            # Add ALL transitive deps for full coverage in tsconfig paths.
            for transitive_info in npm_info.transitive_deps.to_list():
                trans_name = transitive_info.package_name
                if trans_name not in pkg_info_map and transitive_info.package_dir:
                    pkg_info_map[trans_name] = transitive_info

            # Collect transitive package.json files as a depset (no to_list).
            transitive_package_dir_sets.append(npm_info.transitive_package_dirs)

    # Step 1b: build a map from runtime package name → @types package dir.
    # When a package like 'react' has a separate @types/react package, TypeScript
    # must resolve 'react' to the @types/react directory (since react itself ships
    # no .d.ts files).  The pairing shows up as a declaration file outside the
    # runtime package's own directory, which is the @types/* package dir.
    #
    # Read for every package in `paths`, not only the direct deps: an untyped
    # package reached transitively (vitest -> @vitest/expect -> chai) is named in
    # `paths` all the same, and a paths entry pointing at a package that ships no
    # declarations resolves to no types at all.
    types_override = {}  # pkg_name → @types_dir (when a types dep is paired)
    for pkg_name, npm_info in pkg_info_map.items():
        if pkg_name.startswith("@types/"):
            continue  # @types/* packages don't need an override
        runtime_pkg_dir = npm_info.package_dir.dirname

        # One package's own declarations, not a transitive closure.
        for dts_file in npm_info.declaration_files.to_list():
            if not dts_file.path.startswith(runtime_pkg_dir):
                types_override[pkg_name] = dts_file.dirname
                break

    # Step 1c: build npm_pkg_dirs from pkg_info_map using types_override.
    # npm_pkg_dirs entries: (pkg_name, pkg_dir_or_file_path, is_file)
    #   When is_file is True, pkg_dir_or_file_path points directly to a .d.ts file
    #   (from exports_types_file). This generates a more precise paths entry like:
    #     "pkg": ["path/to/index.d.ts"]
    #   rather than:
    #     "pkg": ["path/to/pkg/dir"]
    npm_pkg_dirs = []
    for pkg_name, npm_info in pkg_info_map.items():
        pkg_dir = npm_info.package_dir.dirname

        # Override with @types/* dir when the runtime package has separate types.
        if pkg_name in types_override:
            pkg_dir = types_override[pkg_name]
            npm_pkg_dirs.append((pkg_name, pkg_dir, False))
        elif npm_info.exports_types_file:
            # Package has conditional exports with a 'types' entry.
            # Point directly at the .d.ts file for precise resolution.
            npm_pkg_dirs.append((pkg_name, npm_info.exports_types_file.path, True))
        else:
            npm_pkg_dirs.append((pkg_name, pkg_dir, False))

    dep_dts_depset = depset(transitive = transitive_dts_sets, order = "postorder")
    dep_globals_depset = depset(transitive = global_entry_sets, order = "postorder")

    # module_name deps: every module reachable from here, direct or not, since a
    # bare specifier in a dep's .d.ts has to resolve too.
    module_sets = [
        dep[TsModuleInfo].transitive_modules
        for dep in ctx.attr.deps
        if TsModuleInfo in dep
    ]
    module_paths = depset(transitive = module_sets).to_list()

    # No deps, no closure to arrive through: nothing an import could resolve to
    # that a direct dep does not provide.
    scan_srcs = compile_srcs + js_srcs + passthrough_dts
    strict_deps = None
    if ctx.attr.deps and scan_srcs:
        strict_deps = _strict_deps_check(
            ctx = ctx,
            scan_srcs = scan_srcs,
            own_files = ctx.files.srcs + ctx.files.path_alias_srcs,
            direct_provided = depset(transitive = direct_provided_sets),
            transitive_provided = depset(transitive = (
                transitive_dts_sets + transitive_js_sets + transitive_css_sets +
                transitive_css_module_sets + transitive_asset_sets
            )),
            npm_direct = sorted(direct_npm_names),
            npm_reachable = [
                _npm_hub_entry(pkg_info_map[name])
                for name in sorted(pkg_info_map)
            ],
            module_direct = sorted([
                dep[TsModuleInfo].module_name
                for dep in ctx.attr.deps
                if TsModuleInfo in dep and dep[TsModuleInfo].module_name
            ]),
            module_reachable = module_paths,
        )
    strict_deps_inputs = [strict_deps.stamp] if strict_deps else []
    strict_deps_gated = False

    # The rest of the user's `extends` chain. Starlark cannot read the tsconfig
    # to follow it, so a ts_config target declares it and we make every file in
    # it an action input.
    tsconfig_chain = []
    baseline_file = None
    if ctx.file.tsconfig:
        baseline_file = _write_baseline_tsconfig(ctx)
        tsconfig_chain = [ctx.file.tsconfig, baseline_file]
        if TsConfigInfo in ctx.attr.tsconfig:
            tsconfig_chain += ctx.attr.tsconfig[TsConfigInfo].deps_tsconfigs.to_list()

    # Who emits the .d.ts decides what each action is on the hook for.
    #   "oxc":  oxc emits declarations syntactically, which REQUIRES isolated
    #           declarations; tsgo then only reports diagnostics, so checking
    #           stays off the critical path.
    #   "tsgo": oxc transpiles JS only and tsgo emits declarations from the full
    #           program, so no source annotations are required.
    #
    # enable_check = False under "tsgo" means there is no type program at all,
    # and therefore no declarations: an opt-out of types, not of correctness.
    # That is the right shape for terminal targets -- app entry points, dev
    # servers, bundle inputs -- whose types nothing consumes.
    oxc_emits_dts = ctx.attr.declarations == "oxc"
    tsgo_emits_dts = not oxc_emits_dts and ctx.attr.enable_check
    emits_dts = oxc_emits_dts or tsgo_emits_dts

    if ctx.attr.declaration_map and not tsgo_emits_dts:
        fail(
            "ts_compile: declaration_map on {} needs the tsgo declaration emit.\n".format(ctx.label) +
            "oxc writes declarations syntactically and emits no map for them, and a " +
            "target that emits no declarations has nothing to map.\nSet declarations = " +
            "\"tsgo\" with enable_check = True, or drop declaration_map.",
        )

    # ── Declare outputs ───────────────────────────────────────────────────
    #
    # Every output keeps its package-relative path, so a target may hold a
    # subtree. oxc's --strip-dir-prefix takes a single value, so the sources are
    # grouped by the root their package-relative path hangs off (the package
    # directory for a checked-in file, the bin directory for a generated one)
    # and each group gets its own invocation.
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

        js_out = ctx.actions.declare_file(stem + ".js")
        js_outputs.append(js_out)
        group_outs.append(js_out)
        if ctx.attr.source_map:
            js_map_out = ctx.actions.declare_file(stem + ".js.map")
            js_map_outputs.append(js_map_out)
            group_outs.append(js_map_out)
        if emits_dts:
            dts_out = ctx.actions.declare_file(stem + ".d.ts")
            dts_outputs.append(dts_out)
            if oxc_emits_dts:
                group_outs.append(dts_out)
        if ctx.attr.declaration_map:
            dts_map_outputs.append(ctx.actions.declare_file(stem + ".d.ts.map"))

    # JavaScript needs no transform, so it is staged in the output tree as-is:
    # a relative import of it from compiled TypeScript has to resolve at runtime
    # in the same directory layout.
    js_passthrough = []
    for src in js_srcs:
        rel = _package_relative_path(src, pkg)
        staged = ctx.actions.declare_file(rel)
        ctx.actions.symlink(output = staged, target_file = src)
        js_passthrough.append(staged)
        if tsgo_emits_dts:
            stem = rel[:-(len(src.extension) + 1)]
            dts_ext = _JS_DECLARATION_EXTENSION[src.extension]
            dts_outputs.append(ctx.actions.declare_file(stem + dts_ext))
            if ctx.attr.declaration_map:
                dts_map_outputs.append(ctx.actions.declare_file(stem + dts_ext + ".map"))

    all_outputs = js_outputs + js_map_outputs + dts_outputs + dts_map_outputs + js_passthrough

    # ── Compile actions ───────────────────────────────────────────────────
    for root in sorted(oxc_srcs_by_root.keys()):
        args = ctx.actions.args()
        args.add("--files")
        args.add_all(oxc_srcs_by_root[root])
        args.add("--out-dir", out_base)
        if root:
            args.add("--strip-dir-prefix", root)

        args.add("--target", ctx.attr.target)
        if ctx.attr.jsx_mode:
            args.add("--jsx", ctx.attr.jsx_mode)
        if ctx.attr.source_map:
            args.add("--source-map")
        if oxc_emits_dts:
            args.add("--declaration")
            args.add("--isolated-declarations")

        strict_deps_gated = True
        ctx.actions.run(
            inputs = depset(oxc_srcs_by_root[root] + strict_deps_inputs, transitive = [dep_dts_depset]),
            outputs = oxc_outs_by_root[root],
            executable = oxc.oxc_binary,
            arguments = [args],
            mnemonic = "OxcCompile",
            progress_message = "OxcCompile %{label}",
        )

    # ── tsgo action: declaration emit, or diagnostics only ────────────────
    #
    # This action already had to construct the complete program -- every
    # transitive .d.ts and every npm package.json -- to type-check. In tsgo mode
    # it keeps the declarations it computes instead of discarding them, which is
    # what removes the isolated-declarations requirement at near-zero cost.
    program_srcs = compile_srcs + js_srcs
    validation_outputs = []
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if tsgo_emits_dts and not tsgo_toolchain_info and program_srcs:
        fail(
            "ts_compile: declarations = \"tsgo\" needs a tsgo toolchain, and none " +
            "is registered.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")\n" +
            "Or set declarations = \"oxc\" to emit declarations with oxc instead " +
            "(which requires an explicit type on every export).",
        )
    if tsgo_toolchain_info and ctx.attr.enable_check and program_srcs:
        tsgo = tsgo_toolchain_info.tsgo_info

        emit_root_dir = None
        if tsgo_emits_dts:
            roots = {}
            for src in program_srcs:
                roots[_source_root(src, pkg)] = True
            root_list = sorted(roots.keys())
            if len(root_list) > 1:
                fail(
                    "ts_compile: srcs on {} hang off {} different roots, and one ".format(
                        ctx.label,
                        len(root_list),
                    ) +
                    "declaration emit has one rootDir:\n  " + "\n  ".join(root_list) + "\n" +
                    "A target may hold a whole subtree, but not a mix of checked-in and " +
                    "generated sources. Put the generated sources in their own ts_compile " +
                    "target and depend on it, or set declarations = \"oxc\" (or " +
                    "enable_check = False), neither of which emits from tsgo.",
                )
            emit_root_dir = root_list[0]

        # Include .ts/.tsx sources, JavaScript sources and ambient .d.ts files
        # in the tsconfig — ambient declarations provide type context for
        # checking.
        check_srcs = compile_srcs + js_srcs + passthrough_dts
        tsconfig = _generate_tsconfig(
            ctx = ctx,
            srcs = check_srcs,
            npm_pkg_dirs = npm_pkg_dirs if npm_pkg_dirs else None,
            ambient_dts = [ambient_dts[path] for path in sorted(ambient_dts)] +
                          dep_globals_depset.to_list(),
            module_paths = module_paths,
            extends_file = ctx.file.tsconfig,
            baseline_file = baseline_file,
            allow_js = bool(js_srcs),
            emit_declarations = tsgo_emits_dts,
            emit_root_dir = emit_root_dir,
            emit_out_dir = out_base if tsgo_emits_dts else None,
        )

        # Build the depset of transitive npm package.json files so that
        # moduleResolution:"Bundler" can read exports/types fields from each
        # package. This must be computed before the action is registered.
        npm_pkg_dirs_depset = depset(transitive = transitive_package_dir_sets)

        strict_deps_gated = True
        tsgo_inputs = depset(
            check_srcs + [tsconfig, tsgo.tsgo_binary] + tsconfig_chain +
            ctx.files.path_alias_srcs + strict_deps_inputs,
            transitive = [dep_dts_depset, dep_globals_depset, npm_pkg_dirs_depset],
        )
        if not tsgo_emits_dts:
            # Diagnostics only. Stays in the _validation output group so it runs
            # concurrently with downstream compilation.
            stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
            check_args = ctx.actions.args()
            check_args.add("-stamp", stamp)
            check_args.add("--")
            check_args.add(tsgo.tsgo_binary)
            check_args.add("--project", tsconfig)
            check_args.add_all(ctx.attr.tsgo_args)
            check_args.add("--noEmit")
            ctx.actions.run(
                inputs = tsgo_inputs,
                outputs = [stamp],
                executable = ctx.executable._tsaction,
                arguments = ["stamp", check_args],
                mnemonic = "TsgoCheck",
                progress_message = "TsgoCheck %{label}",
            )
            validation_outputs.append(stamp)
        else:
            # The .d.ts files are real outputs, so a type error fails the build
            # by construction -- no --output_groups=+_validation needed, and a
            # broken target cannot hand a stale declaration to a consumer.
            tsgo_args = ctx.actions.args()
            tsgo_args.add("--project", tsconfig.path)
            tsgo_args.add_all(ctx.attr.tsgo_args)
            ctx.actions.run(
                inputs = tsgo_inputs,
                outputs = dts_outputs + dts_map_outputs,
                executable = tsgo.tsgo_binary,
                arguments = [tsgo_args],
                mnemonic = "TsgoDeclare",
                progress_message = "TsgoDeclare %{label}",
            )

    # A target with sources but no compile action of its own -- JavaScript srcs
    # with checking off -- has nothing to hang the stamp on, so it goes in the
    # output group Bazel requests for every target in the build.
    if strict_deps and not strict_deps_gated:
        validation_outputs.append(strict_deps.stamp)

    # ── Build providers ───────────────────────────────────────────────────
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

    # ts_compile produces no CSS and no assets of its own, so it only forwards
    # what its deps carry: the direct fields stay empty and the closure travels
    # in the transitive ones.
    transitive_css = depset(transitive = transitive_css_sets, order = "postorder")
    transitive_css_modules = depset(transitive = transitive_css_module_sets, order = "postorder")
    transitive_css_exports = depset(transitive = transitive_css_exports_sets, order = "postorder")
    transitive_assets = depset(transitive = transitive_asset_sets, order = "postorder")

    own_global_entries = [_global_dts_entry(ctx, passthrough_dts)] if passthrough_dts else []

    providers = [
        # This target's own outputs. A dep's files reach a consumer through the
        # provider that describes them, not through this one.
        DefaultInfo(files = depset(all_outputs + passthrough_dts)),
        JsInfo(
            js_files = direct_js,
            js_map_files = direct_js_map,
            transitive_js_files = transitive_js,
            transitive_js_map_files = transitive_js_map,
        ),
        TsDeclarationInfo(
            declaration_files = direct_dts,
            transitive_declaration_files = transitive_dts,
            global_entry_files = depset(own_global_entries),
            transitive_global_entry_files = depset(
                own_global_entries,
                transitive = global_entry_sets,
                order = "postorder",
            ),
        ),
    ]

    # Derived from bin_dir rather than from a declared File so that a target
    # with no sources of its own still forwards its deps' modules.
    declaration_root = out_base
    source_root = "/".join([
        p
        for p in [ctx.label.workspace_root, pkg]
        if p
    ])
    own_modules = []
    if ctx.attr.module_name:
        own_modules.append(struct(
            module_name = ctx.attr.module_name,
            label = _label_text(ctx.label),
            declaration_root = declaration_root,
            source_root = source_root,
            declared_paths = (),
        ))
    providers.append(TsModuleInfo(
        module_name = ctx.attr.module_name,
        label = _label_text(ctx.label),
        declaration_root = declaration_root,
        source_root = source_root,
        declared_paths = (),
        transitive_modules = depset(own_modules, transitive = module_sets),
    ))

    # Always propagate CssInfo so ts_compile targets can be used as CSS deps.
    providers.append(CssInfo(
        css_files = depset(),
        transitive_css_files = transitive_css,
    ))

    # Propagate CssModuleInfo so ts_compile targets can carry CSS Module deps.
    providers.append(CssModuleInfo(
        css_files = depset(),
        transitive_css_files = transitive_css_modules,
        exports_files = depset(),
        transitive_exports_files = transitive_css_exports,
    ))

    # Propagate AssetInfo so ts_compile targets can carry asset deps.
    providers.append(AssetInfo(
        asset_files = depset(),
        transitive_asset_files = transitive_assets,
    ))

    output_groups = {}
    if validation_outputs:
        output_groups["_validation"] = depset(validation_outputs)
    if strict_deps:
        # Requesting this group alone checks every target's deps without
        # compiling anything, since the check reads only the target's own srcs.
        output_groups["strict_deps"] = depset([strict_deps.stamp, strict_deps.checker])
    if output_groups:
        providers.append(OutputGroupInfo(**output_groups))

    return providers

# ─── Rule declaration ──────────────────────────────────────────────────────────

ts_compile = rule(
    implementation = _ts_compile_impl,
    attrs = {
        "srcs": attr.label_list(
            doc = """Sources to compile.

.ts / .tsx      compiled by oxc; one .js (+ .js.map, + .d.ts) output each.
.js / .mjs/.cjs staged into the output tree unchanged and added to the type
                program. allowJs is set for them, so JSDoc types cross the
                package boundary; add checkJs through compiler_options to have
                them type-checked. Under declarations = "tsgo" each one also
                gets a declaration (.d.ts / .d.mts / .d.cts), the same as tsc.
.d.ts           ambient declarations: type context for the check, passed
                straight through to consumers, never in `include`. One with no
                top-level import or export declares globals, and those are in
                scope in every target that depends on this one.

Paths are kept relative to the target's package, so srcs may span a subtree.
""",
            allow_files = [".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mjs", ".cjs"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Other ts_compile, ts_npm_package, css_library, css_module, asset_library, or json_library targets that this target depends on.",
            providers = [[TsDeclarationInfo, JsInfo], [TsDeclarationInfo], [CssInfo], [CssModuleInfo], [AssetInfo]],
        ),
        "target": attr.string(
            doc = "ECMAScript target version passed to oxc-bazel (e.g. 'es2022').",
            default = "es2022",
        ),
        "jsx_mode": attr.string(
            doc = "JSX transform mode: 'react-jsx', 'react', 'preserve'. Empty disables JSX.",
            default = "react-jsx",
        ),
        "declarations": attr.string(
            doc = """Which tool emits the .d.ts files.

"tsgo" (default): the tsgo action emits declarations from the full type
program. Source needs no explicit export annotations, and the declarations are
exactly what tsc would produce. Type errors fail the build because the .d.ts
are real outputs. Type-checking is on the critical path.

"oxc": oxc emits declarations syntactically, per file, without a type program.
This REQUIRES isolated declarations -- every export needs an explicit type --
and oxc errors if that does not hold. In exchange, type-checking moves off the
critical path into the _validation output group, so downstream targets compile
while checking runs concurrently.""",
            default = "tsgo",
            values = ["tsgo", "oxc"],
        ),
        "enable_check": attr.bool(
            doc = "Run tsgo type-checking as a validation action (requires tsgo toolchain).",
            default = True,
        ),
        "source_map": attr.bool(
            doc = """Emit one .js.map next to every .js oxc writes.

Off drops the outputs and the flag, for a target whose JavaScript nothing
debugs -- a codegen step, or a bundle input whose bundler makes its own map.""",
            default = True,
        ),
        "declaration_map": attr.bool(
            doc = """Emit a .d.ts.map next to every declaration.

This is what makes go-to-definition across a package boundary land on the
.ts source instead of the generated .d.ts. Needs the tsgo declaration emit
(declarations = "tsgo" with enable_check = True); oxc emits no map.""",
            default = False,
        ),
        "tsgo_args": attr.string_list(
            doc = """Extra flags for the tsgo invocation.

Only flags that report on the program are accepted -- --traceResolution,
--explainFiles, --listFiles, --listEmittedFiles, --diagnostics,
--extendedDiagnostics, --noErrorTruncation. A compilerOption belongs in
compiler_options instead, where the Bazel-owned-key guard can see it; any other
flag would move an output this rule already declared to Bazel.""",
        ),
        "tsconfig": attr.label(
            doc = """The project's own tsconfig.json, used as the compilerOptions baseline.

Either a .json file or a ts_config target (which additionally declares the
files the tsconfig `extends`). The file is referenced where it lives, not
copied, so relative paths inside it keep resolving against the directory they
were written for.

The generated tsconfig `extends` this file and overrides the options Bazel owns
(see _BAZEL_OWNED_OPTIONS) plus paths and include. Everything else -- lib, the
strict* family, verbatimModuleSyntax and the rest -- is whatever the file says,
so tsgo checks the code under the same options `tsc` would.

Setting this attribute adds the file's options; it never takes the ruleset's
baseline away. strict, module Preserve, skipLibCheck and esModuleInterop apply
either way, and with a `tsconfig` they sit UNDER it: every one of them the file
(or its own extends chain) mentions comes from the file, and only the ones it
says nothing about fall back to the baseline.

moduleResolution is the exception, asserted only when no `tsconfig` sets a
`module` the ruleset cannot see: TypeScript couples the two, so a baseline
value would outlive the module it belongs to. tsgo derives the resolver from
whichever `module` wins, which is Bundler for all of them but Node16/NodeNext.""",
            allow_single_file = [".json"],
        ),
        "compiler_options_json": attr.string(
            doc = """JSON object of compilerOptions overrides, on top of `tsconfig`.

Set through the ts_compile macro's lib / types / target / jsx_mode /
jsx_import_source / compiler_options arguments rather than written by hand.
Entries in `types` and `typeRoots` are treated as relative to the target's
package, matching how they are written in a package's own tsconfig.json.""",
        ),
        "module_name": attr.string(
            doc = """The bare specifier this target is importable as, e.g. "@acme/ui".

Dependents get a paths entry mapping this name (and its subpaths) to the .d.ts
files this target produces, wherever the current configuration puts them. The
entry point is index.d.ts.""",
        ),
        "path_aliases": attr.string_dict(
            doc = """Source-level path alias mappings to inject into the tsgo tsconfig.

Maps alias prefixes (as they appear in import statements) to workspace-relative
**source** directory paths. These are added to the compilerOptions.paths section
of the generated tsconfig so that tsgo can resolve path aliases that are defined
in the project's tsconfig.json (compilerOptions.paths cannot be inherited from
it: paths is one key, and the rule owns it).

An alias must resolve to files this target already stages -- its own srcs, or
files listed in path_alias_srcs. An alias pointing anywhere else is an analysis
error, because the action would resolve it against whatever another action
happened to leave in the sandbox. A value pointing into bazel-out/ is rejected
for the same reason: to make a bare specifier resolve to another target's
generated declarations, set module_name on that target and depend on it.

Examples:
    # tsconfig.json has: {"@/*": ["./src/*"]}, and this target compiles src/.
    path_aliases = {"@/": "src/"}

    # The aliased files belong to another target.
    path_aliases = {"@lib/": "packages/lib/src/"}
    path_alias_srcs = ["//packages/lib/src:sources"]
""",
        ),
        "_tsaction": attr.label(
            default = Label("//ts/tools/tsaction"),
            executable = True,
            cfg = "exec",
        ),
        "path_alias_srcs": attr.label_list(
            doc = """Files a path_aliases entry resolves to, when they are not in srcs.

They become inputs to the type-check action, which is what makes an alias into
another target's sources resolve the same way every time. tsgo type-checks them
as part of this program, so a type error in one of them fails this target: a
dep with module_name is the cheaper boundary where one is available.""",
            allow_files = True,
        ),
    },
    toolchains = [
        OXC_TOOLCHAIN_TYPE,
        config_common.toolchain_type(TSGO_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Compiles TypeScript source files using oxc-bazel.

Produces one .js, .js.map, and .d.ts output per .ts/.tsx input file, and stages
every .js/.mjs/.cjs input into the output tree as-is. Output paths stay relative
to the target's package, so srcs may span a subtree.

The .d.ts outputs are the compilation boundary: downstream ts_compile targets
only depend on the .d.ts files, enabling fine-grained Bazel caching.

When a tsgo toolchain is registered, type-checking runs as a validation
action in the _validation output group — it executes during `bazel build`
but does not block downstream targets.

Compiler options come from `tsconfig` (the project's own file, whatever it
says) and from `compiler_options_json`, in that order, with the options Bazel
owns applied last. Use the ts_compile macro in //ts:defs.bzl rather than this
rule directly: it takes lib / types / compiler_options as Starlark values.
""",
)
