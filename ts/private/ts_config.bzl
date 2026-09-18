"""ts_config: a hand-written tsconfig.json, the files it extends, its jsx and
its module."""

load("//ts/private:providers.bzl", "TsConfigInfo")

def _ts_config_impl(ctx):
    chain = []
    chain_sets = []
    for dep in ctx.attr.deps:
        if TsConfigInfo in dep:
            chain.append(dep[TsConfigInfo].tsconfig)
            chain_sets.append(dep[TsConfigInfo].deps_tsconfigs)
        else:
            chain.extend(dep[DefaultInfo].files.to_list())

    return [
        # Exactly the one file, so ts_compile's allow_single_file tsconfig attr
        # resolves to it; the chain travels in the provider instead.
        DefaultInfo(files = depset([ctx.file.src])),
        TsConfigInfo(
            tsconfig = ctx.file.src,
            deps_tsconfigs = depset(chain, transitive = chain_sets),
            jsx = ctx.attr.jsx,
            module = ctx.attr.module,
        ),
    ]

ts_config = rule(
    implementation = _ts_config_impl,
    attrs = {
        "src": attr.label(
            doc = "The tsconfig.json to use as a compilerOptions baseline.",
            allow_single_file = [".json"],
            mandatory = True,
        ),
        "deps": attr.label_list(
            doc = "Every file `src` extends, transitively: .json files or other ts_config targets.",
            allow_files = [".json"],
        ),
        "jsx": attr.string(
            doc = """`"preserve"` when the chain's effective `jsx` is that.

tsc names a `.tsx`'s emit `.jsx` under `preserve` and `.js` under every other
mode, and a rule declares its outputs before any action can read the file, so
the one value that names an output is declared here. Gazelle writes it from the
`extends` chain; the `TsConfig` action fails a target with a `.tsx` src when
the declaration and the file disagree, naming the edit.""",
            values = ["", "preserve"],
        ),
        "module": attr.string(
            doc = """The chain's effective `module` when it is one tsgo emits --
`"commonjs"`, `"node16"`, `"node18"`, `"nodenext"` -- lowercased as tsgo prints
it; unset for an ES kind or `preserve`.

A vitest test runs ES modules whatever the program's `module`, so a ts_compile
whose program tsgo emits also emits the ES twin of each `.js`, and a rule
declares its outputs before any action can read the file: the value that says
the twins exist is declared here. Gazelle writes it from the `extends` chain;
the `TsConfig` action fails a target with a `.ts` src when the declaration and
the file disagree, naming the edit.""",
        ),
    },
    doc = """Declares a tsconfig.json: the files it extends, its jsx and its
module.

Starlark cannot read the file, so what a rule needs from it before any action
runs is declared here: the `extends` chain, every file of which becomes an
input to the type-check action, `jsx = "preserve"` when that is the chain's
effective value, since it names a `.tsx`'s emit, and `module` when the chain's
is one tsgo emits, since it names the ES twins a vitest test runs. Pass the
result to ts_compile's `tsconfig` attr. A tsconfig that extends nothing and
sets neither can be passed to ts_compile directly, without this rule.

Example:
    ts_config(
        name = "tsconfig",
        src = "tsconfig.json",
        deps = ["//:tsconfig.base.json"],
    )

    ts_compile(
        name = "lib",
        srcs = ["index.ts"],
        tsconfig = ":tsconfig",
    )
""",
)
