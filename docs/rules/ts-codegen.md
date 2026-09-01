# ts_codegen

Runs a generator executable that reads sources and writes TypeScript, as a
declared build action. It is the TypeScript equivalent of
`proto_library` → `go_proto_library`: a `ts_compile` takes declared files in
`srcs`, and a declared directory (`out_dir`) in `deps`.

```python
load("@rules_typescript//ts:defs.bzl", "ts_binary", "ts_codegen", "ts_compile")

ts_binary(
    name = "gen_routes",
    entry_point = "generate-routes.mjs",
)

ts_codegen(
    name = "route_tree",
    srcs = glob(["src/routes/**/*.tsx"]),
    outs = ["src/routeTree.gen.ts"],
    args = ["--routes-dir", "{srcs_dir}", "--out", "{out}"],
    generator = ":gen_routes",
)

ts_compile(
    name = "route_tree_ts",
    srcs = [":route_tree"],
    declarations = "oxc",
)

ts_compile(
    name = "app",
    srcs = ["src/main.tsx"],
    deps = [":route_tree_ts"],
)
```

`ts_binary` runs the `.mjs` on the JS runtime toolchain, so the generator is the
script and nothing else. A generator that imports npm packages at runtime
additionally takes `node_modules` — see
[The environment the generator gets](#the-environment-the-generator-gets).

The generated sources are their own `ts_compile` target, and it does not use the
default declaration emit — see [Compiling the output](#compiling-the-output).

Gazelle auto-detects Prisma, GraphQL codegen and OpenAPI generators from
`package.json` and writes the target itself; the `# gazelle:ts_codegen`
directive is for a generator it does not recognise. See
[Register a codegen target](../gazelle/directives.md#register-a-codegen-target).
It writes the `ts_compile` that consumes the output too, named
`<name>_compile`, and resolves imports of the generated module to it. A
checked-in file a `ts_codegen` declares as an out is kept out of the package's
`srcs`, so the two cannot both claim it. A checked-in `*.gen.ts` no rule
declares is an ordinary source: nothing in the build writes it, and leaving it
out is a module its importers cannot find. `routeTree.gen.ts` is the exception
-- the Start Vite plugin writes that one. Use
[`# gazelle:ts_exclude`](../gazelle/directives.md#exclude-generated-files)
for one you want out anyway.

## Compiling the Output

A generated file lives in the output tree, and the default emit
(`declarations = "tsgo"`, `enable_check = True`) cannot take it from there. Two
failures:

**Checked-in and generated sources in one target fail at analysis.** One tsgo
declaration emit has one `rootDir`, and those two sets hang off different roots:

```
ts_compile: srcs on //src/app:app hang off 2 different roots, and one
declaration emit has one rootDir:
  bazel-out/k8-fastbuild/bin/src/app
  src/app
```

**Generated sources alone fail inside the tsgo action.** Under that emit
`outDir` is the package's directory in `bazel-bin`, which is where the generated
source already is, and TypeScript's implicit `exclude` covers `outDir`, so the
program comes out empty:

```
error TS18003: No inputs were found in config file
'.../route_tree_ts.tsconfig.json'. Specified 'include' paths were
'["src/routeTree.gen.ts"]' ...
```

The target holding generated sources picks another emitter:

- **`declarations = "oxc"`** — oxc emits the `.d.ts` syntactically, per file,
  with no type program, so downstream targets still type-check against the
  generated code. It requires an explicit type on every export, which the
  generator's output has to satisfy.
- **`enable_check = False`** — no type program and therefore no `.d.ts` at all,
  for generated code whose types nothing downstream consumes. `//tests/codegen`
  uses it.

`declarations = "oxc"` also lifts the first error, so one target holding both
sets is expressible: oxc groups its sources by root and runs once per group.

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `srcs` | `label_list` | required | The files the generator reads. Empty is an analysis-time error |
| `outs` | `output_list` | `[]` | The files the generator writes, declared. The generator must write exactly these |
| `out_dir` | `string` | `""` | A single declared **directory** instead, for a generator that produces a tree it will not enumerate (Prisma's client, say) |
| `generator` | `label` | required | The executable, built for the exec configuration |
| `args` | `string_list` | `[]` | The generator's command line, after placeholder substitution |
| `node_modules` | `label` | `None` | An npm tree for a generator that imports packages at runtime. Name the target `node_modules` if the generator uses ESM |
| `module_name` | `string` | `""` | The bare specifier the `out_dir` tree is importable as. Requires `out_dir` |
| `env` | `string_dict` | `{}` | Extra environment for the action |

`outs` and `out_dir` are **mutually exclusive, and exactly one is required**.
Both being unset and both being set are separate analysis-time errors. Bazel
requires every output to be declared at analysis time, so a generator whose
output set depends on its input is only expressible as `out_dir`.

`module_name` requires `out_dir` — see [A directory of output](#a-directory-of-output).

## A Directory of Output

A generator whose file names come from its input — one module per message
bundle, per Prisma model, per GraphQL operation — cannot have its outputs
declared. `out_dir` declares the directory instead, and the target then carries
what a `ts_compile` reads a dep through:

```python
ts_codegen(
    name = "messages",
    srcs = ["project.inlang/settings.json"] + glob(["messages/*.json"]),
    out_dir = "compiled",
    args = ["--project", "{srcs_dir}", "--outdir", "{out}"],
    generator = ":compile_messages",
    module_name = "#app/messages",
    node_modules = ":node_modules",
)

ts_compile(
    name = "app",
    srcs = ["main.ts"],
    deps = [":messages"],
)
```

`main.ts` imports `#app/messages`, and the declarations inside the tree type it.

**The tree goes in `deps`, never in `srcs`.** `srcs` declares one output per
input file at analysis time, and a directory has no file list until its action
has run; putting one there is an analysis-time error naming this attribute. So
the generator has to emit **compiled** output — `.js` beside `.d.ts` — because
nothing downstream will compile it. A generator that emits `.ts` sources into a
tree has no route today.

`module_name` is the only way to import out of the tree by name. Without it the
tree is still staged for the consumer's type-check, but no `paths` entry points
at it and the import does not resolve:

```
error TS2307: Cannot find module '#app/messages' or its corresponding type
declarations.
```

A relative import into the tree works too — `./compiled/messages/greeting.js`
from a source in the same package — and needs no `module_name`. The
undeclared-import check resolves it against the directory rather than against a
file list it does not have, so it still names the label when the tree arrives
only through another dep.

## Placeholders in `args`

Substituted into each argument string before the action runs. All paths are
execroot-relative.

| Placeholder | Expands to |
|---|---|
| `{srcs_dir}` | the directory of the **first** src |
| `{srcs}` | every src path, space-separated in one argument |
| `{out}` | the path of the first declared output — the `out_dir` directory when `out_dir` is set |
| `{outs_dir}` | the directory of the first declared output |
| `{node_modules_dir}` | the npm tree's path; only substituted when `node_modules` is set |

`{srcs_dir}` and `{outs_dir}` are the first entry's directory, not a common
ancestor. A `glob()` spanning two directories hands the generator one of them; a
generator that needs the whole set takes `{srcs}`.

`{srcs}` becomes a single argument containing every path, space-separated, so a
generator taking a list needs a shell wrapper that word-splits it.

## The Environment the Generator Gets

Three variables the rule sets so a generator script need not know any path:

| Variable | When | Value |
|---|---|---|
| `NODE_BINARY` | a `js_tool` toolchain is registered | the toolchain node. Set with `setdefault`, so an `env` entry of your own wins |
| `NODE_PATH` | `node_modules` is set | the tree's directory, for CJS resolution |
| `TS_CODEGEN_NODE_MODULES` | `node_modules` is set | the same path, for a script that forks a child process |

**A generator that writes bare ESM imports needs the target named literally
`node_modules`.** The tree's directory is named after its target, and Node's
ESM resolver only ever looks in a directory called `node_modules` as it walks
up — `NODE_PATH` is a CJS mechanism and ESM ignores it. So
`node_modules = ":codegen_node_modules"` leaves the generator failing at
runtime:

```
Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'consola'
```

Name the target `node_modules`. One per package is the limit that follows; a
second npm tree in the same package can only serve a CJS generator.

The shape for a Node generator is a [`ts_binary`](ts-binary.md) whose
`entry_point` is the script itself. The rule resolves the runtime from the JS
runtime toolchain and locates the entry through the runfiles library, so nothing
depends on `node` being on `PATH` and nothing has to read `NODE_BINARY`:

```python
ts_binary(
    name = "gen_schema",
    entry_point = "generate-schema.mjs",
    data = ["schema-helpers.mjs"],
)
```

Sibling modules the entry imports go in `data`, which is what puts them in
runfiles beside it.

`NODE_BINARY` still reaches the generator's environment, for a generator that
forks a child Node process of its own.

A generator that is not a Node program at all — a Go binary, a Rust binary, a
shell script — is any executable target, `sh_binary` included. `sh_binary`
carries its own `load`: it is a `rules_shell` rule, not a built-in, and a BUILD
file that omits the line fails to load with `name 'sh_binary' is not defined`.
Locate a script inside a shell wrapper with the Bash runfiles library and
`rlocation`; `"$0.runfiles"` does not exist when Bazel hands the action a
runfiles manifest instead of a tree:

```bash
#!/usr/bin/env bash
# source @bazel_tools//tools/bash/runfiles first; it defines rlocation.
exec "$NODE_BINARY" "$(rlocation _main/path/to/script.mjs)" "$@"
```
