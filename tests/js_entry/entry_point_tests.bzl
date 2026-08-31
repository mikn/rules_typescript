"""Analysis-time coverage for the entry_point shapes ts_binary refuses.

entry_point is polymorphic, so the attr can no longer declare `providers`, and
the diagnostic is the rule's own. A TypeScript source gets its own message: the
rule will not compile it, and the fix is a ts_compile rather than a rename.
"""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

def _fails_with(message):
    def _impl(ctx):
        env = analysistest.begin(ctx)
        asserts.expect_failure(env, message)
        return analysistest.end(env)

    return analysistest.make(_impl, expect_failure = True)

typescript_entry_point_test = _fails_with("is a TypeScript source, which this rule does not compile")
unusable_entry_point_test = _fails_with("does not provide JsInfo and is not a JavaScript file")
