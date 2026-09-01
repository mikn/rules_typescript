"""The tags every nested-Bazel integration test carries.

One definition rather than one per package: three hand-copied copies had already
drifted, and the copy nobody remembered kept `exclusive` after the others
dropped it -- which put it back in the fast lane on its own.
"""

load(
    "@rules_bazel_integration_test//bazel_integration_test:defs.bzl",
    "integration_test_utils",
)

# integration_test_utils.DEFAULT_INTEGRATION_TEST_TAGS is ["exclusive", "manual"].
# Both are dropped deliberately.
#
# `manual` took these tests out of every wildcard, which meant nothing ever ran
# them.
#
# `exclusive` serialised all of them, which was the whole cost of the
# integration lane: 18 nested Bazel invocations one after another. Nothing in
# them shares mutable state -- the harness gives each its own workspace, output
# base and scratch dir under its own TEST_TMPDIR, which `no-sandbox` below makes
# a real directory and which Bazel keys by target and by output base, the two
# runners that bind a port take a kernel-assigned one, and the shared repository
# cache is content-addressed and safe for concurrent Bazel servers. What it was
# buying is two things, covered here instead: a bound on how many nested servers
# run at once, which "cpu:2" states directly so Bazel derives it from the
# machine, and -- measured -- the fact that cold, concurrent servers all miss
# the shared repository cache at once and all fetch the same ~4GB, which is why
# the CI job restores it before running.
_BASE_TAGS = [
    tag
    for tag in integration_test_utils.DEFAULT_INTEGRATION_TEST_TAGS
    if tag not in ("manual", "exclusive")
] + [
    # Lane membership, kept separate from scheduling: this is what
    # --test_tag_filters=-nested-bazel excludes from the fast lane. `exclusive`
    # used to do both jobs, so dropping it silently pulled 18 nested Bazel
    # invocations into the unit-test job.
    "nested-bazel",
    "cpu:2",
    "no-sandbox",
    # These point a nested Bazel at this source tree via RULES_TS_ROOT, so the
    # ruleset's .bzl files are read without being action inputs -- and glob()
    # cannot collect them into one filegroup, since every dir is a subpackage.
    "external",
]

# "cpu:2" bounds the nested servers on ONE machine; these spread the suite over
# several. Each name has a `test:ci-integration-<name>` config in //.bazelrc
# selecting it by tag, and a leg in the integration-tests matrix running that
# config.
#
# A test with no shard is not left out: the `core` leg's filter is the
# complement of every name here, so the shards only ever move tests OFF the
# default leg and cannot drop one. Balance the legs by cost, not by count --
# nextjs_test alone is ~293s.
SHARDS = [
    "nextjs-tanstack",
    "remix-svelte",
    "npm",
]

def nested_bazel_tags(shard = None):
    """Tags for one nested-Bazel integration test.

    Args:
        shard: which CI leg runs it, or None for the default `core` leg.

    Returns:
        The tag list to pass as the test's `tags`.
    """
    if shard == None:
        return _BASE_TAGS
    if shard not in SHARDS:
        fail(
            "unknown integration shard %r. Did you mean one of %s? A new leg " % (shard, SHARDS) +
            "needs three edits: the name here, a test:ci-integration-<name> " +
            "config in //.bazelrc (and its exclusion from the core leg's " +
            "filter), and a matrix entry in .github/workflows/ci.yml.",
        )
    return _BASE_TAGS + ["shard-" + shard]
