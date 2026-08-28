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
# base and scratch dir under scratchRoot()/<name>, the two runners that bind a
# port take a kernel-assigned one, and the shared repository cache is
# content-addressed and safe for concurrent Bazel servers. What it was buying is
# two things, covered here instead: a bound on how many nested servers run at
# once, which "cpu:2" states directly so Bazel derives it from the machine, and
# -- measured -- the fact that cold, concurrent servers all miss the shared
# repository cache at once and all fetch the same ~4GB, which is why the CI job
# restores it before running.
NESTED_BAZEL_TAGS = [
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
