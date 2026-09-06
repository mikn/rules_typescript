"""Core TypeScript compilation rule using oxc-bazel.

ts_compile transforms .ts/.tsx source files into .js + .js.map + .d.ts outputs
using the oxc-bazel CLI as a Bazel action.

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
not block downstream compilation.

The rule has three attributes: srcs, deps and tsconfig. Every compiler option is
the tsconfig's; the emit knobs are the build flags //ts:declarations (tsgo|oxc),
//ts:source_map, //ts:declaration_map and //ts:lib_check.
"""

load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")
load("//ts/private:node_modules.bzl", "build_node_modules_action", "collect_npm_packages")
load("//ts/private:providers.bzl", "AssetInfo", "CssInfo", "CssModuleInfo", "JsInfo", "NpmPackageInfo", "TsConfigInfo", "TsDeclarationInfo")
load("//ts/private:runtime.bzl", "JS_TOOL_TOOLCHAIN_TYPE", "get_js_tool")
load("//ts/private:toolchain.bzl", "OXC_TOOLCHAIN_TYPE", "TSGO_TOOLCHAIN_TYPE", "get_oxc_toolchain")

# ─── Helpers ──────────────────────────────────────────────────────────────────

_TS_EXTENSIONS = ["ts", "tsx"]

_JS_EXTENSIONS = ["js", "mjs", "cjs"]

# tsc's own naming for the declaration it emits from a JavaScript source.
_JS_DECLARATION_EXTENSION = {
    "js": ".d.ts",
    "mjs": ".d.mts",
    "cjs": ".d.cts",
}

_DECLARATION_SUFFIXES = (".d.ts", ".d.mts", ".d.cts")

def _is_dts_source(f):
    """Returns True if the file is a declaration file."""
    return f.basename.endswith(_DECLARATION_SUFFIXES)

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

def explicitly_relative(path):
    """A `paths` value spelled so TypeScript reads it as a path, not a package.

    `_relative_path` answers with a bare segment whenever the target sits under
    the tsconfig's own directory, and tsgo removed `baseUrl`, so TypeScript reads
    that as a module specifier and rejects it with TS5090. TypeScript's own test
    for an already-relative path is `^\\.\\.?($|/)` -- which is why a leading
    dot alone does not qualify: `.bazel/npm/x` is a directory named `.bazel`, not
    a relative path. Exported for the unit test.
    """
    if path in (".", "..") or path.startswith("./") or path.startswith("../") or path.startswith("/"):
        return path
    return "./" + path

def subpath_roots(tsconfig_dir, pkg_root, entry_rel_dir):
    """Where `pkg/sub` may live, in the order npm would look.

    With no `exports` map -- which is most of the registry -- `pkg/sub` is a
    plain path under the package root, so `recharts/types/shape/Curve` is
    `<recharts>/types/shape/Curve`. Hanging the wildcard off the entry's own
    directory instead spells that `<recharts>/types/types/shape/Curve`. The
    entry directory stays as a second substitution: a package whose subpaths do
    sit beside its entry keeps resolving, and TypeScript tries each in turn.
    Exported for the unit test.
    """
    return subpath_wildcards(
        explicitly_relative(_relative_path(tsconfig_dir, pkg_root)),
        entry_rel_dir,
    )

def subpath_wildcards(pkg_root_rel, entry_rel_dir):
    """`subpath_roots` over two directories already relative to the tsconfig.

    tsconfig_aspect reaches the same two by its own route -- the installed tree
    under `npm_dir` rather than an external repository -- and which order they
    go in is the rule above, not a second opinion about it. Exported for that
    caller and for the unit test.
    """
    roots = [pkg_root_rel] if pkg_root_rel == entry_rel_dir else [pkg_root_rel, entry_rel_dir]
    return [r + "/*" for r in roots]

def subpath_pattern_paths(pkg_root_rel, entry_rel_dir, key, target):
    """`paths` values for one `exports` pattern key, the manifest's answer first.

    `key` is the `exports` key (`./utils/*`) and `target` the package-relative
    pattern it maps to (`dist/types/utils/*.d.ts`); TypeScript substitutes the
    matched star into the whole value, so both keep their prefix and suffix.
    Behind the answer come the guesses `subpath_wildcards` makes for `<pkg>/*`,
    each spelled with the key's own shape, so a manifest naming a directory the
    tarball lacks resolves no worse than one nobody read. Exported for
    tsconfig_aspect and the unit test.
    """
    values = [pkg_root_rel + "/" + target]
    for root in subpath_wildcards(pkg_root_rel, entry_rel_dir):
        guess = root[:-len("/*")] + key[1:]
        if guess not in values:
            values.append(guess)
    return values

def types_package_alias(package_name):
    """The name `@types/x` supplies declarations for, or None for any other package.

    DefinitelyTyped publishes `x`'s declarations as `@types/x`, and a scoped
    `@a/b`'s as `@types/a__b`. TypeScript pairs the two by walking
    `node_modules/@types`, which this ruleset does not have: npm packages reach
    the compiler through `paths`, and a key spelled `@types/x` answers no import
    anyone writes. Exported for the unit test.
    """
    if not package_name.startswith("@types/"):
        return None
    unmangled = package_name[len("@types/"):]
    scope, separator, name = unmangled.partition("__")
    return "@" + scope + "/" + name if separator else unmangled

def types_package_name(package_name):
    """The `@types/*` package DefinitelyTyped publishes `package_name`'s declarations as.

    The inverse of `types_package_alias`: `@a/b` is `@types/a__b`. Exported for
    the unit test.
    """
    if package_name.startswith("@"):
        return "@types/" + package_name[1:].replace("/", "__", 1)
    return "@types/" + package_name

# ─── Tsconfig generation ─────────────────────────────────────────────────────

# The options a TypeScript target gets from this ruleset whether or not it names
# a `tsconfig`: a file the action config extends FIRST, so every key the user's
# tsconfig (or its own extends chain) mentions wins and only the keys it says
# nothing about fall back here. moduleResolution is not among them: TypeScript
# rejects a resolver it did not derive from the `module` that wins (TS5109,
# TS5110), and tsgo derives Bundler from every module but Node16/NodeNext.
# allowArbitraryExtensions is required by the .d.ts this ruleset generates for
# css_module, css_library, asset_library and json_library deps.
_BASELINE_OPTIONS = {
    "strict": True,
    "module": "Preserve",
    "target": "es2022",
    "jsx": "react-jsx",
    "skipLibCheck": True,
    "esModuleInterop": True,
    "allowArbitraryExtensions": True,
}

# A tsconfig `types` entry names a package, and TypeScript resolves it by walking
# node_modules for that package and reading its manifest. The editor's tsconfig
# (tsconfig_aspect.bzl) has no node_modules to walk, so the entry is resolved
# here for it, against what the package's own manifest designated, and the file
# goes in the editor's `files`.
#
# Gazelle reads the same shapes out of a `types` entry, in `ambientTypeLabel`
# (gazelle/config.go), for the other half of the job: it reads the entries out of
# a tsconfig file and writes the npm deps, while the rule reads the attribute and
# resolves it against those deps. Different inputs, one vocabulary -- an entry
# the two classify differently is either one the rule ignores while Gazelle
# writes a dep for it, silently back to the bug the guard below exists for, or a
# package the rule demands a dep for that Gazelle never writes: a fail() nothing
# can clear. So one table of shapes is asserted on both sides:
# `types_entry_package_ref_test` in //tests/compiler_options/analysis and
# `TestTsConfigTypes_EntryShapesAreClassifiedLikeTheRule` in //gazelle.
def types_entry_package_ref(entry):
    """The package one `compilerOptions.types` entry names, or "".

    The recogniser of this attribute for the whole ruleset: anything that has
    to know what a `types` entry names calls this rather than spelling the
    shapes again, and `types_entry_file` below is the one resolution built on
    it. A second spelling is a second answer for some entry, and the guard
    below turns a disagreement into a fail() no dep clears.

    "" for an entry that names a path instead: one starting with `.` or `/`, or
    ending in a declaration extension, which no dep resolves.
    `types_entry_declaration` below takes the two of those shapes the rule
    resolves itself. Whitespace is trimmed first, which is what Gazelle's
    `ambientTypeLabel` does before it reads the same shapes -- so a padded entry
    it writes a dep for is one this spends that dep on, and a blank entry, which
    it writes no dep for, trims away to no package at all.
    """
    entry = entry.strip()
    if entry.startswith(".") or entry.startswith("/") or entry.endswith(_DECLARATION_SUFFIXES):
        return ""
    return entry

# `^\.\.?($|/)` is TypeScript's own test for a path here; this takes the `./` and
# `../` of it that end in a declaration extension and leaves the rest --
# `vendor/x.d.ts`, `.`, `./typings` -- to the compiler.
def types_entry_declaration(entry):
    """The declaration file one `types` entry names, package-relative, or "".

    Paired with `types_entry_package_ref` above: between them they classify
    every entry, one to a dep and one to a label of this target's, and an entry
    both answer "" for is the compiler's own to resolve.

    A relative entry that does not end in a declaration extension is one of
    those: `./typings` is a directory whose declarations TypeScript picks by
    reading it, which Starlark cannot do.
    """
    entry = entry.strip()
    if not entry.startswith("./") and not entry.startswith("../"):
        return ""
    return entry if entry.endswith(_DECLARATION_SUFFIXES) else ""

def workspace_relative(package, entry):
    """`entry`, written relative to `package`, as a path from the workspace root.

    "" when its `..` segments climb out above the root, which no input answers.
    """
    parts = []
    for part in (package.split("/") if package else []) + entry.split("/"):
        if part in ("", "."):
            continue
        if part != "..":
            parts.append(part)
        elif parts:
            parts.pop()
        else:
            return ""
    return "/".join(parts)

def types_entry_file(entry, npm_info):
    """The declaration `entry` designates in `npm_info`, or None.

    The resolution for the whole ruleset, the way `types_entry_package_ref` is
    the classification: calling these two is how a second reader of the
    attribute stays the same reader, rather than a copy that comes to disagree
    about one entry.

    Four package spellings resolve, each one TypeScript would have walked
    node_modules for: the package itself; one of its `exports` subpaths; a
    subpath its manifest says nothing about, answered by the declaration the
    package ships there, so `@cloudflare/workers-types/2023-07-01` is that
    package's `2023-07-01/index.d.ts`; and the bare name a paired @types/*
    package supplies -- `types = ["node"]` is @types/node, which is the only
    place DefinitelyTyped puts it.

    For the third, TypeScript consults the manifest three ways before it reads
    a file, and NpmPackageInfo carries none of the three: a `typesVersions`
    mapping, a package.json inside the subpath's directory, and whether an
    `exports` map exists at all, since one that omits the subpath stops tsc.
    This reads the shipped files alone, in tsc's order
    (`_shipped_subpath_candidates`). Where a manifest maps the subpath in
    `typesVersions` the two part: web-streams-polyfill rewrites `dist/types/*`
    to `dist/types/ts3.6/*`, so `web-streams-polyfill/dist/types/polyfill` is
    `dist/types/ts3.6/polyfill.d.ts` to tsc and `dist/types/polyfill.d.ts`
    here. The unit test pins that answer.
    """
    ref = types_entry_package_ref(entry)
    if not ref:
        return None
    name = npm_info.package_name
    if ref == name:
        return npm_info.exports_types_file or npm_info.ambient_types_file
    if ref.startswith(name + "/"):
        subpath = ref[len(name):]
        return npm_info.subpath_types.get("." + subpath) or _shipped_subpath_file(subpath[1:], npm_info)
    if types_package_alias(name) == ref:
        return npm_info.ambient_types_file
    return None

def _shipped_subpath_candidates(sub, npm_info):
    """Every file `pkg/<sub>` may resolve to among the package's own, in TypeScript's order.

    typeRoots are read before node_modules is walked, so `<sub>/index.d.ts` under
    the paired @types package outranks everything the package itself ships, and
    that package's `<sub>.d.ts` comes last. `shown` is the path as the package
    publishes it, for the message an unresolved entry fails with.
    """
    name, own = npm_info.package_name, npm_info.package_root
    file, index = "/" + sub + ".d.ts", "/" + sub + "/index.d.ts"
    shipped = [(own + file, name + file), (own + index, name + index)]
    if npm_info.types_package_dir:
        types, types_name = npm_info.types_package_dir.dirname, types_package_name(name)
        shipped = [(types + index, types_name + index)] + shipped + [(types + file, types_name + file)]
    return [struct(path = path, shown = shown) for path, shown in shipped]

def _shipped_subpath_file(sub, npm_info):
    if not sub:
        return None
    ranked = {c.path: rank for rank, c in enumerate(_shipped_subpath_candidates(sub, npm_info))}
    best = None

    # One package's own declarations, and only for a subpath its manifest left
    # unnamed: a depset answers "which file sits at this path" no other way.
    for f in npm_info.declaration_files.to_list():
        rank = ranked.get(f.path)
        if rank != None and (best == None or rank < best[0]):
            best = (rank, f)
    return best[1] if best else None

def _directive_answer(name, deps):
    """The dep of a package that answers its `/// <reference types="name" />`, and the file.

    TypeScript's order: `@types/<name>` under typeRoots first, a package called
    `name` beside it second -- both against the referencing package's own
    dependencies, which is where npm installs them.
    """
    typed = [dep for dep in deps if dep.package_name.startswith("@types/")]
    for dep in typed + [dep for dep in deps if not dep.package_name.startswith("@types/")]:
        designated = types_entry_file(name, dep)
        if designated:
            return dep, designated
    return None, None

# A worklist bound Starlark's for-loop needs, not a size any chain approaches.
_MAX_REFERENCED_DECLARATIONS = 1024

def referenced_type_files(entry, npm_info):
    """`entry` and every declaration its `/// <reference types=...>` directives reach.

    A `@types/*` entry in `files` brings its own declarations; what it
    references arrives only if something resolves the directive, and tsgo
    cannot -- the resolver walks typeRoots and node_modules, never `paths`. So
    each name the package recorded for the file is answered from that package's
    own deps, and the answer's directives are followed in turn: @types/bun is
    one line forwarding to bun-types, whose entry references `node`. Items are
    structs of `file` and the `package` (NpmPackageInfo) it belongs to, `entry`
    first.
    """
    out = [struct(file = entry, package = npm_info)]
    seen = {entry.path: True}
    for i in range(_MAX_REFERENCED_DECLARATIONS):
        if i >= len(out):
            return out
        item = out[i]
        for name in item.package.type_references.get(item.file.path, []):
            dep, designated = _directive_answer(name, item.package.direct_deps)
            if designated and designated.path not in seen:
                seen[designated.path] = True
                out.append(struct(file = designated, package = dep))
    fail("ts_compile: more than {} declarations reached through /// <reference types> directives from {}".format(
        _MAX_REFERENCED_DECLARATIONS,
        entry.path,
    ))

def _write_baseline_tsconfig(ctx):
    """Writes _BASELINE_OPTIONS as a tsconfig for the action's config to extend.

    A file rather than a dict merge because Starlark cannot read the user's
    tsconfig to see which keys it already sets; TypeScript resolves that itself,
    and an `extends` list is the only place a layer can sit *under* the file.
    """
    out = ctx.actions.declare_file("{}.tsconfig_baseline.json".format(ctx.label.name))
    ctx.actions.write(
        output = out,
        content = json.encode_indent({"compilerOptions": _BASELINE_OPTIONS}, indent = "  "),
    )
    return out

# ─── Undeclared imports ──────────────────────────────────────────────────────
#
# An import has to be satisfied by a DIRECT dep. Inputs stay transitive and
# resolution becomes direct, and the split has to happen here rather than in the
# forest: one node_modules serves the whole program, so leaving a transitive
# package out would also stop a declared dep's own .d.ts from resolving ITS
# imports, which widens those types to `any` instead of reporting anything.
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
  scan: [],
  own: new Set(),
  direct: new Set(),
  transitive: new Map(),
  provided: [],
  transitiveDirs: new Map(),
  npmDirect: new Set(),
  npmTransitive: new Map(),
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
    case "scan": cfg.scan.push(field[1]); break;
    case "own": for (const k of keysOf(field[1])) cfg.own.add(k); break;
    case "direct": for (const k of keysOf(field[1])) cfg.direct.add(k); break;
    case "own-dir": case "direct-dir": cfg.provided.push(stripBin(field[1])); break;
    case "transitive-dir":
      if (!cfg.transitiveDirs.has(stripBin(field[1]))) {
        cfg.transitiveDirs.set(stripBin(field[1]), field[2]);
      }
      break;
    case "transitive":
      for (const k of keysOf(field[1])) {
        if (!cfg.transitive.has(k)) cfg.transitive.set(k, field[2]);
      }
      break;
    case "npm-direct": cfg.npmDirect.add(field[1]); break;
    case "npm-transitive": cfg.npmTransitive.set(field[1], field[2]); break;
  }
}

// ── Classification ───────────────────────────────────────────────────────────

function undeclared(specifier, file) {
  // The fragment starts at the FIRST # only when something precedes it: a
  // leading one makes the whole specifier a package-imports name.
  const query = specifier.split("?")[0];
  const hash = query.indexOf("#", 1);
  const clean = hash > 0 ? query.slice(0, hash) : query;
  if (clean === "") return null;

  if (clean.startsWith("./") || clean.startsWith("../")) {
    const dir = stripBin(file).split("/").slice(0, -1).join("/");
    const resolved = normalize(dir + "/" + clean);
    const candidates = [resolved, stripExtension(resolved)];
    for (const c of [...candidates]) candidates.push(c + "/index");
    for (const c of candidates) {
      if (cfg.own.has(c) || cfg.direct.has(c)) return null;
    }
    // A directory answers for everything under it: whether the file is really
    // there is the compiler's question, not this one's.
    for (const dir of cfg.provided) {
      if (underPrefix(resolved, dir)) return null;
    }
    for (const c of candidates) {
      if (cfg.transitive.has(c)) return cfg.transitive.get(c);
    }
    for (const [dir, label] of cfg.transitiveDirs) {
      if (underPrefix(resolved, dir)) return label;
    }
    return null;
  }

  if (clean.startsWith("node:")) return null;
  const pkg = packageOf(clean);
  if (builtins.has(pkg) || cfg.npmDirect.has(pkg)) return null;
  if (cfg.npmTransitive.has(pkg)) return cfg.npmTransitive.get(pkg);
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

def label_text(label):
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

# A directory travels as its own path, and expansion is off wherever these are
# added: the check's inputs are the target's own srcs, so a dep's tree is one it
# holds no input for and Bazel fails the action rather than expanding it.
def _own_manifest_entry(f):
    kind = "own-dir" if f.is_directory else "own"
    return "{}\t{}".format(kind, f.path)

def _direct_manifest_entry(f):
    kind = "direct-dir" if f.is_directory else "direct"
    return "{}\t{}".format(kind, f.path)

def _transitive_manifest_entry(f):
    owner = f.owner

    # An external-repo file is an npm package's, and no relative specifier in a
    # first-party source reaches one; the npm entries below carry those by name.
    if not owner or owner.repo_name:
        return None
    kind = "transitive-dir" if f.is_directory else "transitive"
    return "{}\t{}\t{}".format(kind, f.path, label_text(owner))

def _strict_deps_check(
        ctx,
        scan_srcs,
        own_files,
        direct_provided,
        transitive_provided,
        npm_direct,
        npm_reachable):
    """Registers the action that fails on an import no direct dep provides.

    Args:
        ctx:                 Rule context.
        scan_srcs:           This target's own sources, the files to read.
        own_files:           Files this target already stages: its srcs.
        direct_provided:     depset of File: what the direct deps produce.
        transitive_provided: depset of File: the whole closure, for the label
                             an undeclared import has to be attributed to.
        npm_direct:          npm package names of the direct deps.
        npm_reachable:       struct(name, label) per npm package in the closure.

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
    manifest.add("label\t" + label_text(ctx.label))
    manifest.add("bin\t" + ctx.bin_dir.path)
    for name in npm_direct:
        manifest.add("npm-direct\t" + name)
    for pkg in npm_reachable:
        if pkg.name not in npm_direct:
            manifest.add("npm-transitive\t{}\t{}".format(pkg.name, pkg.label))

    manifest.add_all(scan_srcs, format_each = "scan\t%s")
    manifest.add_all(own_files, map_each = _own_manifest_entry, expand_directories = False)
    manifest.add_all(direct_provided, map_each = _direct_manifest_entry, expand_directories = False)
    manifest.add_all(transitive_provided, map_each = _transitive_manifest_entry, expand_directories = False)

    ctx.actions.run(
        inputs = depset(scan_srcs + [checker]),
        outputs = [stamp],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [checker.path, stamp.path, manifest],
        mnemonic = "TsStrictDeps",
        progress_message = "TsStrictDeps %{label}",
    )
    return struct(stamp = stamp, checker = checker)

# ─── Attribute validation ────────────────────────────────────────────────────

def _classify_srcs(ctx):
    """Splits srcs into the TypeScript, JavaScript and ambient-declaration sets."""
    compile_srcs = []
    js_srcs = []
    passthrough_dts = []
    for f in ctx.files.srcs:
        if f.is_directory:
            fail(
                "ts_compile: '{}' on {} is a directory.\n".format(f.short_path, ctx.label) +
                "srcs declares one output per file at analysis time, and a directory has " +
                "no file list until its action has run.\nA tree of already-compiled output " +
                "-- ts_codegen(out_dir = ...) -- belongs in deps, where it is staged whole " +
                "and reached through the tsconfig's `paths`.",
            )
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
                "ts_compile: srcs must contain only .ts, .tsx, .js, .mjs, .cjs, " +
                ".d.ts, .d.mts or .d.cts files; got '{}' (extension: .{}).\n".format(f.short_path, f.extension) +
                "Remove this file from srcs, or if you need to pass through assets " +
                "use a filegroup or a dedicated rule for that file type.\n" +
                "Did you mean to add it to a different attribute?",
            )
    return compile_srcs, js_srcs, passthrough_dts

# ─── Rule implementation ───────────────────────────────────────────────────────

def _ts_compile_impl(ctx):
    oxc = get_oxc_toolchain(ctx)
    pkg = ctx.label.package

    compile_srcs, js_srcs, passthrough_dts = _classify_srcs(ctx)

    # Collect transitive deps. An npm dep contributes no declaration files: its
    # files reach tsgo through the forest, and a copy staged at its own exec
    # path would be a second module of the same name.
    transitive_dts_sets = []
    dep_npm_closure_sets = []
    transitive_js_sets = []
    transitive_js_map_sets = []
    transitive_css_sets = []
    transitive_css_module_sets = []
    transitive_css_exports_sets = []
    transitive_asset_sets = []

    # What the direct deps produce themselves, which is the set an import has to
    # be satisfied from. The transitive sets above stay the action inputs.
    direct_provided_sets = []

    direct_npm_infos = []
    direct_npm_names = {}

    for dep in ctx.attr.deps:
        if NpmPackageInfo in dep:
            npm_info = dep[NpmPackageInfo]
            direct_npm_infos.append(npm_info)
            direct_npm_names[npm_info.package_name] = True
        elif TsDeclarationInfo in dep:
            transitive_dts_sets.append(dep[TsDeclarationInfo].transitive_declaration_files)
            dep_npm_closure_sets.append(dep[TsDeclarationInfo].transitive_npm_packages)
            direct_provided_sets.append(dep[TsDeclarationInfo].declaration_files)
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

    # The forest: every package this target's imports or its deps' declarations
    # can name, one entry per resolution, the target's own deps flat. A dep's
    # emitted .d.ts imports the packages the dep declared.
    forest_packages = collect_npm_packages(
        direct_npm_infos + depset(transitive = dep_npm_closure_sets, order = "postorder").to_list(),
    )

    # One entry per name for the undeclared-import check, the direct deps'
    # resolution first. A workspace member has no package_dir: a consumer names
    # its view directly, and npm_direct answers for it.
    reachable_by_name = {}
    for npm_info in forest_packages:
        if npm_info.package_dir and npm_info.package_name not in reachable_by_name:
            reachable_by_name[npm_info.package_name] = npm_info

    dep_dts_depset = depset(transitive = transitive_dts_sets, order = "postorder")

    # No deps, no closure to arrive through: nothing an import could resolve to
    # that a direct dep does not provide.
    scan_srcs = compile_srcs + js_srcs + passthrough_dts
    strict_deps = None
    if ctx.attr.deps and scan_srcs:
        strict_deps = _strict_deps_check(
            ctx = ctx,
            scan_srcs = scan_srcs,
            own_files = ctx.files.srcs,
            direct_provided = depset(transitive = direct_provided_sets),
            transitive_provided = depset(transitive = (
                transitive_dts_sets + transitive_js_sets + transitive_css_sets +
                transitive_css_module_sets + transitive_asset_sets
            )),
            npm_direct = sorted(direct_npm_names),
            npm_reachable = [
                _npm_hub_entry(reachable_by_name[name])
                for name in sorted(reachable_by_name)
            ],
        )
    strict_deps_inputs = [strict_deps.stamp] if strict_deps else []
    strict_deps_gated = False

    # The chain the action config extends: the ruleset's baseline, then the
    # user's file and what it extends. Starlark cannot read the file to follow
    # its chain, so a ts_config target declares it and every file in it is an
    # action input.
    baseline_file = _write_baseline_tsconfig(ctx)
    tsconfig_chain = [baseline_file]
    if ctx.file.tsconfig:
        tsconfig_chain.append(ctx.file.tsconfig)
        if TsConfigInfo in ctx.attr.tsconfig:
            tsconfig_chain += ctx.attr.tsconfig[TsConfigInfo].deps_tsconfigs.to_list()

    # Who emits the .d.ts decides what each action is on the hook for.
    #   "oxc":  oxc emits declarations syntactically, which REQUIRES isolated
    #           declarations; tsgo then only reports diagnostics, so checking
    #           stays off the critical path.
    #   "tsgo": oxc transpiles JS only and tsgo emits declarations from the full
    #           program, so no source annotations are required.
    oxc_emits_dts = ctx.attr._declarations[BuildSettingInfo].value == "oxc"
    tsgo_emits_dts = not oxc_emits_dts
    source_map = ctx.attr._source_map[BuildSettingInfo].value
    declaration_map = ctx.attr._declaration_map[BuildSettingInfo].value

    if declaration_map and not tsgo_emits_dts:
        fail(
            "ts_compile: --//ts:declaration_map needs the tsgo declaration emit, and " +
            "--//ts:declarations=oxc takes it away.\noxc writes declarations " +
            "syntactically and emits no map for them.\nBuild with one flag or the other.",
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
        if source_map:
            js_map_out = ctx.actions.declare_file(stem + ".js.map")
            js_map_outputs.append(js_map_out)
            group_outs.append(js_map_out)
        dts_out = ctx.actions.declare_file(stem + ".d.ts")
        dts_outputs.append(dts_out)
        if oxc_emits_dts:
            group_outs.append(dts_out)
        if declaration_map:
            dts_map_outputs.append(ctx.actions.declare_file(stem + ".d.ts.map"))

    # JavaScript needs no transform, so it is staged in the output tree as-is:
    # a relative import of it from compiled TypeScript has to resolve at runtime
    # in the same directory layout.
    # TypeScript keeps the higher-priority extension of a .mjs / .d.mts pair listed
    # together, so the .mjs leaves the program and tsgo writes nothing for it.
    checked_in_dts = {_package_relative_path(d, pkg): True for d in passthrough_dts}
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
                dts_map_outputs.append(ctx.actions.declare_file(dts_rel + ".map"))

    # JavaScript srcs whose declarations are all checked in leave tsgo nothing
    # to write: a program to check, not one to emit from.
    tsgo_emits_dts = tsgo_emits_dts and bool(dts_outputs)

    all_outputs = js_outputs + js_map_outputs + dts_outputs + dts_map_outputs + js_passthrough

    program_srcs = compile_srcs + js_srcs
    check_srcs = compile_srcs + js_srcs + passthrough_dts
    tsgo_toolchain_info = ctx.toolchains[TSGO_TOOLCHAIN_TYPE]
    if program_srcs and not tsgo_toolchain_info:
        fail(
            "ts_compile: {} needs a tsgo toolchain, and none is registered.\n".format(ctx.label) +
            "The tsconfig the actions read, and the target and jsx oxc transforms with, " +
            "come from `tsgo --showConfig`.\nAdd to MODULE.bazel:\n" +
            "    register_toolchains(\"@rules_typescript//ts/toolchain:all\")",
        )

    # ── The action tsconfig ───────────────────────────────────────────────
    #
    # Written by tsaction from the chain above and tsgo's own reading of it, so
    # tsgo and oxc take target and jsx from one place.
    tsconfig = None
    options_file = None
    if program_srcs:
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
                    "declaration emit has one rootDir:\n  " + "\n  ".join([r or "the exec root" for r in root_list]) + "\n" +
                    "A target may hold a whole subtree, but not a mix of checked-in and " +
                    "generated sources. Put the generated sources in their own ts_compile " +
                    "target and depend on it, or build with --//ts:declarations=oxc, " +
                    "which emits nothing from tsgo.",
                )
            emit_root_dir = root_list[0]

        tsconfig = ctx.actions.declare_file("{}.tsconfig.json".format(ctx.label.name))
        options_file = ctx.actions.declare_file("{}.options.json".format(ctx.label.name))
        config_args = ctx.actions.args()
        config_args.use_param_file("@%s", use_always = False)
        config_args.set_param_file_format("multiline")
        config_args.add(tsgo.tsgo_binary, format = "-tsgo=%s")
        if ctx.file.tsconfig:
            config_args.add(ctx.file.tsconfig, format = "-tsconfig=%s")
        config_args.add(baseline_file, format = "-baseline=%s")
        config_args.add(tsconfig, format = "-out=%s")
        config_args.add(options_file, format = "-options=%s")
        config_args.add(ctx.bin_dir.path, format = "-bin_dir=%s")
        config_args.add_all(
            sorted([name[len("@types/"):] for name in direct_npm_names if name.startswith("@types/")]),
            format_each = "-types_dep=%s",
        )
        if tsgo_emits_dts:
            config_args.add("-emit")
            config_args.add(out_base, format = "-out_dir=%s")
            config_args.add(emit_root_dir, format = "-root_dir=%s")
        if declaration_map:
            config_args.add("-declaration_map")
        if oxc_emits_dts:
            config_args.add("-isolated_declarations")
        if ctx.attr._lib_check[BuildSettingInfo].value:
            config_args.add("-lib_check")
        config_args.add_all(check_srcs)
        ctx.actions.run(
            inputs = depset(
                check_srcs + [tsgo.tsgo_binary] + tsconfig_chain,
                transitive = [dep_dts_depset],
            ),
            outputs = [tsconfig, options_file],
            executable = ctx.executable._tsaction,
            arguments = ["tsconfig", config_args],
            mnemonic = "TsConfig",
            progress_message = "TsConfig %{label}",
        )

    # ── Compile actions ───────────────────────────────────────────────────
    for root in sorted(oxc_srcs_by_root.keys()):
        args = ctx.actions.args()
        args.add("--files")
        args.add_all(oxc_srcs_by_root[root])
        args.add("--out-dir", out_base)
        if root:
            args.add("--strip-dir-prefix", root)
        if source_map:
            args.add("--source-map")
        if oxc_emits_dts:
            args.add("--declaration")
            args.add("--isolated-declarations")

        strict_deps_gated = True
        ctx.actions.run(
            inputs = depset(oxc_srcs_by_root[root] + strict_deps_inputs + [options_file], transitive = [dep_dts_depset]),
            outputs = oxc_outs_by_root[root],
            executable = ctx.executable._tsaction,
            tools = [oxc.oxc_binary],
            arguments = ["oxc", "-options=" + options_file.path, "--", oxc.oxc_binary.path, args],
            mnemonic = "OxcCompile",
            progress_message = "OxcCompile %{label}",
        )

    # ── tsgo action: declaration emit, or diagnostics only ────────────────
    #
    # The program is the srcs, the first-party deps' declarations and the
    # forest, run from a program root that mirrors the exec root with the forest
    # at its node_modules -- see the module docstring.
    validation_outputs = []
    if program_srcs:
        forest = build_node_modules_action(
            ctx,
            forest_packages,
            [npm_info.all_files for npm_info in forest_packages],
            output_name = "{}/node_modules".format(ctx.label.name),
        )
        strict_deps_gated = True
        tsgo_inputs = depset(
            check_srcs + [tsconfig, forest, tsgo.tsgo_binary] + tsconfig_chain + strict_deps_inputs,
            transitive = [dep_dts_depset],
        )
        run_args = ctx.actions.args()
        run_args.add("-root={}/{}.program".format(tsconfig.dirname, ctx.label.name))
        run_args.add("-node_modules=" + forest.path)
        stamp = None
        if not tsgo_emits_dts:
            # Diagnostics only. Stays in the _validation output group so it runs
            # concurrently with downstream compilation.
            stamp = ctx.actions.declare_file("{}.tscheck".format(ctx.label.name))
            run_args.add(stamp, format = "-stamp=%s")
        run_args.add("--")
        run_args.add(tsgo.tsgo_binary)
        run_args.add("--project", tsconfig)
        if stamp:
            run_args.add("--noEmit")
        ctx.actions.run(
            inputs = tsgo_inputs,
            # Under the emit the .d.ts files are real outputs, so a type error
            # fails the build by construction and no stale declaration survives.
            outputs = [stamp] if stamp else dts_outputs + dts_map_outputs,
            executable = ctx.executable._tsaction,
            arguments = ["tsgo", run_args],
            mnemonic = "TsgoCheck" if stamp else "TsgoDeclare",
            progress_message = ("TsgoCheck" if stamp else "TsgoDeclare") + " %{label}",
        )
        if stamp:
            validation_outputs.append(stamp)

    # A target with declarations alone has no compile action to hang the stamp
    # on, so it goes in the output group Bazel requests for every target.
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
            transitive_npm_packages = depset(
                direct_npm_infos,
                transitive = [info.transitive_deps for info in direct_npm_infos] + dep_npm_closure_sets,
                order = "postorder",
            ),
        ),
    ]

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

    # The tsconfig this target hands the compiler, so a test can read the
    # resolution the build sees and compare it against the editor's. Absent on a
    # target with no program to check, which generates none.
    if tsconfig:
        output_groups["tsconfig"] = depset([tsconfig])
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

Paths are kept relative to the target's package, so srcs may span a subtree.
""",
            allow_files = [".ts", ".tsx", ".d.ts", ".d.mts", ".d.cts", ".js", ".jsx", ".mjs", ".cjs"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = """What this target imports: ts_compile, ts_codegen, ts_npm_package, css_library,
css_module, asset_library or json_library targets.

An npm dep reaches tsgo through the node_modules forest, under its package name;
a first-party dep through its declarations, staged under bazel-bin at the paths
the tsconfig's `paths` and their bin-dir twins reach, or through a relative
import; a workspace member through the hub's view of it, `@npm//:<name>`.""",
            providers = [[TsDeclarationInfo, JsInfo], [TsDeclarationInfo], [CssInfo], [CssModuleInfo], [AssetInfo]],
        ),
        "tsconfig": attr.label(
            doc = """The project's own tsconfig.json: where every compiler option comes from.

Either a .json file or a ts_config target (which additionally declares the
files the tsconfig `extends`). The file is referenced where it lives, not
copied, so relative paths inside it keep resolving against the directory they
were written for.

The action's tsconfig extends the ruleset's baseline (strict, module Preserve,
target es2022, jsx react-jsx, skipLibCheck, esModuleInterop,
allowArbitraryExtensions) and then this file, so every key the file or its own
extends chain mentions wins and only the keys it says nothing about fall back
to the baseline. Over both, tsaction sets the keys Bazel owns -- rootDirs,
preserveSymlinks, the emit shape, `include` and `files` -- rewrites `paths` to
the source and bin-dir twins of each value, and rebases each path-shaped
`types` entry to the staged file it names; a `types` entry naming a package
resolves through the forest. oxc transforms with the target, jsx and
jsxImportSource tsgo reads from the same chain.

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
        "_declarations": attr.label(default = Label("//ts:declarations")),
        "_source_map": attr.label(default = Label("//ts:source_map")),
        "_declaration_map": attr.label(default = Label("//ts:declaration_map")),
        "_lib_check": attr.label(default = Label("//ts:lib_check")),
    },
    toolchains = [
        OXC_TOOLCHAIN_TYPE,
        config_common.toolchain_type(TSGO_TOOLCHAIN_TYPE, mandatory = False),
        config_common.toolchain_type(JS_TOOL_TOOLCHAIN_TYPE, mandatory = False),
    ],
    doc = """Compiles TypeScript with oxc and checks it with tsgo.

Produces one .js (+ .js.map under --//ts:source_map, the default) and one .d.ts
per .ts/.tsx input, and stages every .js/.mjs/.cjs input into the output tree
as-is. Output paths stay relative to the target's package, so srcs may span a
subtree.

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
