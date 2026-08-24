"""Exposes the vitest config a ts_test generated, for golden tests."""

def _vitest_config_impl(ctx):
    return [DefaultInfo(files = ctx.attr.test[OutputGroupInfo].vitest_config)]

vitest_config = rule(
    implementation = _vitest_config_impl,
    attrs = {
        "test": attr.label(
            mandatory = True,
            doc = "A ts_test target.",
        ),
    },
    doc = "Collects the `vitest_config` output group of a ts_test target.",
)
