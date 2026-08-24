"""ts_config: a hand-written tsconfig.json and the files it extends."""

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
    },
    doc = """Declares a hand-written tsconfig.json and the files it extends.

Starlark cannot read the file to follow its `extends` chain, so the chain is
declared here and every file in it becomes an input to the type-check action.
Pass the result to ts_compile's `tsconfig` attr. A tsconfig that extends nothing
can be passed to ts_compile directly, without this rule.

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
