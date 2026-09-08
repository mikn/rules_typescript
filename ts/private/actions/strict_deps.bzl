"""The TsStrictDeps action: an import no direct dep provides fails the build.

Checked here, not by thinning the forest: a transitive package missing from
one node_modules widens a declared dep's .d.ts types to `any`, with no error.
"""

load("//ts/private:runtime.bzl", "get_js_tool")

# Embedded rather than a checked-in .mjs so that the manifest format and its one
# reader stay in the same file. Escapes are doubled: it is a Starlark string.
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
  return cfg.bin && p.startsWith(cfg.bin + "/") ? \
p.slice(cfg.bin.length + 1) : p;
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
      while (i.at < source.length && \
!(source[i.at] === "*" && source[i.at + 1] === "/")) {
        if (source[i.at] === "\\n") line += 1;
        i.at += 1;
      }
      i.at += 2;
      continue;
    }
    if (c === "/" && ((lastKind === "punct" && \
!CLOSERS.includes(lastPunct)) || \
(lastKind === "word" && KEYWORDS_BEFORE_REGEX.has(lastWord)))) {
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
        (lastKind === "word" && \
(lastWord === "from" || lastWord === "import")) ||
        (lastKind === "call" && \
(lastWord === "import" || lastWord === "require"));
      if (isSpecifier && value !== "") \
found.push({ specifier: value, line: startLine });
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
  if (arg.startsWith("@")) \
manifest.push(...readFileSync(arg.slice(1), "utf8").split("\\n"));
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
    case "own-dir": case "direct-dir": \
cfg.provided.push(stripBin(field[1])); break;
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
    if (label !== null) \
findings.push({ file: stripBin(file), line, specifier, label });
  }
}

if (findings.length === 0) {
  writeFileSync(stampPath, "");
  process.exit(0);
}

const width = Math.max(...findings.map((f) => `${f.file}:${f.line}`.length));
const lines = [
  `${cfg.label} imports ${findings.length === 1 ? "a module" : "modules"} \
no direct dep provides:`,
  "",
];
for (const f of findings) {
  lines.push(`  ${`${f.file}:${f.line}`.padEnd(width)}  imports \
${JSON.stringify(f.specifier)}`);
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
    """The label as a deps list writes it, the main repo's @@ dropped."""
    text = str(label)
    return text[2:] if text.startswith("@@//") else text

def npm_hub_entry(npm_info):
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

# A directory travels as its own path, expansion off: a dep's tree is not among
# the check's inputs (its own srcs), and Bazel fails an expansion of it.
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

def strict_deps_check(
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
        fail(("ts_compile: checking {} for undeclared imports needs a JS " +
              "tool toolchain, and none is registered.\nAdd to MODULE.bazel:" +
              "\n    register_toolchains(" +
              "\"@rules_typescript//ts/toolchain:all\")").format(ctx.label))

    checker = ctx.actions.declare_file(
        "{}.strictdeps.mjs".format(ctx.label.name),
    )
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
    manifest.add_all(
        own_files,
        map_each = _own_manifest_entry,
        expand_directories = False,
    )
    manifest.add_all(
        direct_provided,
        map_each = _direct_manifest_entry,
        expand_directories = False,
    )
    manifest.add_all(
        transitive_provided,
        map_each = _transitive_manifest_entry,
        expand_directories = False,
    )

    ctx.actions.run(
        inputs = depset(scan_srcs + [checker]),
        outputs = [stamp],
        executable = js_tool.runtime_binary,
        arguments = js_tool.args_prefix + [checker.path, stamp.path, manifest],
        mnemonic = "TsStrictDeps",
        progress_message = "TsStrictDeps %{label}",
    )
    return struct(stamp = stamp, checker = checker)
