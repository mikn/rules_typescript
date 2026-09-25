load("@rules_typescript//ts:defs.bzl", "ts_binary", "ts_codegen", "ts_compile", "ts_config", "ts_refresh_tsconfig", "ts_test")

# gazelle:exclude generated
# gazelle:exclude generate.mjs

ts_binary(
    name = "generator",
    entry_point = "generate.mjs",
)

ts_codegen(
    name = "source",
    srcs = ["names.json"],
    outs = ["generated/value.ts"],
    args = [
        "{srcs}",
        "{out}",
        "source",
    ],
    generator = ":generator",
)

ts_codegen(
    name = "declarations",
    srcs = ["names.json"],
    args = [
        "{srcs}",
        "{out}",
        "tree",
    ],
    generator = ":generator",
    out_dir = "generated/types",
)

ts_compile(
    name = "generated",
    srcs = [
        "authored/value.ts",
        "consumer.ts",
        "override.ts",
        ":source",  # keep
    ],
    emit = True,
    tsconfig = ":tsconfig",
    deps = [":declarations"],
)

ts_test(
    name = "generated_test",
    srcs = ["standalone.test.ts"],
    emit = True,  # keep
    runner = "@rules_typescript//ts/runners:node_test",
    tsconfig = ":tsconfig",
    deps = [
        ":declarations",
        ":generated",
    ],
)

ts_refresh_tsconfig(
    name = "refresh_generated",
    generated_sources = True,
    tsconfig = None,
    deps = [":generated_test"],
)

ts_config(
    name = "tsconfig",
    src = "tsconfig.build.json",  # keep
)
