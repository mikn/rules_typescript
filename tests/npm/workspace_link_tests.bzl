"""Which target the hub depends on for a pnpm `link:` workspace member.

A member is a directory, and which directory inside it holds the target that
compiles it is not the member's to say: `ts_package_boundary` decides it, and
`ts_target_name` renames the result. Both are Gazelle directives, read from BUILD
files that need not exist when the hub is generated -- so the hub enumerates the
directories that could hold the target and looks them up, innermost first,
rather than deriving one from the member's entry point.

The lookup itself is exercised end to end by //tests/npm:boundary_member_consumer
and //tests/npm:leaf_member_consumer, whose two members disagree about which
candidate is the right one. What is pinned here is the candidate list: drop the
entry point's own directory and the second of those members has no target, drop
the member's root and the first one has none.
"""

load("@bazel_skylib//lib:unittest.bzl", "asserts", "unittest")
load("//npm/private:npm_import.bzl", "link_candidate_dirs", "target_name_in")

_CASES = [
    struct(
        shape = "an entry under src/, the ordinary layout",
        path = "packages/workers-wide-event-sink",
        entry = "src/index.ts",
        expected = [
            "packages/workers-wide-event-sink/src",
            "packages/workers-wide-event-sink",
        ],
    ),
    struct(
        shape = "the same, written with a leading ./",
        path = "packages/foo",
        entry = "./src/index.ts",
        expected = ["packages/foo/src", "packages/foo"],
    ),
    struct(
        shape = "an entry at the member root",
        path = "packages/bar",
        entry = "index.ts",
        expected = ["packages/bar"],
    ),
    struct(
        shape = "a member that designates no entry at all",
        path = "packages/baz",
        entry = "",
        expected = ["packages/baz"],
    ),
    struct(
        shape = "a nested entry directory, innermost first",
        path = "web",
        entry = "dist/lib/index.js",
        expected = ["web/dist/lib", "web/dist", "web"],
    ),
]

def _link_candidate_dirs_test(ctx):
    env = unittest.begin(ctx)
    for case in _CASES:
        asserts.equals(
            env,
            case.expected,
            link_candidate_dirs(case.path, case.entry),
            case.shape,
        )
    return unittest.end(env)

def _target_name_test(ctx):
    env = unittest.begin(ctx)
    asserts.equals(
        env,
        "events",
        target_name_in('ts_compile(name = "events")', "packages/events"),
        "the directory basename, which is what Gazelle names a target after",
    )
    asserts.equals(
        env,
        "lib",
        target_name_in(
            '# gazelle:ts_target_name lib\nts_compile(name = "lib")',
            "packages/events",
        ),
        "the directive wins: it is what Gazelle named the target after",
    )
    return unittest.end(env)

link_candidate_dirs_test = unittest.make(_link_candidate_dirs_test)
target_name_test = unittest.make(_target_name_test)

def workspace_link_test_suite(name):
    unittest.suite(name, link_candidate_dirs_test, target_name_test)
