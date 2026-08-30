"""Which target the hub depends on for a pnpm `link:` workspace member.

A member is a directory; what has to be built is the target that compiles its
entry point. Those are the same package only when the entry sits at the member's
root, which is not how a package with a `src/` is laid out -- and `src/` is the
majority of npm.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//npm:lazy.bzl", "link_target_label")

_CASES = [
    struct(
        shape = "an entry under src/, the ordinary layout",
        path = "packages/workers-wide-event-sink",
        entry = "src/index.ts",
        expected = "@@//packages/workers-wide-event-sink/src:src",
    ),
    struct(
        shape = "the same, written with a leading ./",
        path = "packages/foo",
        entry = "./src/index.ts",
        expected = "@@//packages/foo/src:src",
    ),
    struct(
        shape = "an entry at the member root",
        path = "packages/bar",
        entry = "index.ts",
        expected = "@@//packages/bar:bar",
    ),
    struct(
        shape = "a member that designates no entry at all",
        path = "packages/baz",
        entry = "",
        expected = "@@//packages/baz:baz",
    ),
    struct(
        shape = "a nested entry directory",
        path = "web",
        entry = "dist/lib/index.js",
        expected = "@@//web/dist/lib:lib",
    ),
]

def _link_target_label_test(ctx):
    env = unittest.begin(ctx)
    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            link_target_label(case.path, case.entry),
            case.shape,
        )
    return unittest.end(env)

link_target_label_test = unittest.make(_link_target_label_test)

def workspace_link_test_suite(name):
    unittest.suite(name, link_target_label_test)
