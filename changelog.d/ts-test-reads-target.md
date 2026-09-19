### Added

- **`bazel run //path:my_test -- --reads` names the workspace files a test
  opens outside its runfiles.** The vitest runner takes the one argument. It
  runs the tests unsandboxed from the runfiles tree, under a Node `--require`
  hook that records what the test processes open, stat or read, and prints
  every file under the workspace the runfiles do not hold as its
  workspace-relative path and the label a `data` entry would take, sorted,
  once; vitest's own output goes to stderr, and a test reading nothing prints
  nothing. No listing names a run-time read, so Gazelle writes no `data` entry
  for one: the labels are the owner's to declare.
