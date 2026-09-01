### Fixed

- **A dot-directory holding a checked-in `BUILD` file is a package again.** The
  guard added for a directory the generator refuses to walk read that walk as if
  it answered "is this a package", and it does not: it answers "would Gazelle
  ever *write* a BUILD file here". `.github/scripts` in the monorepo this was
  measured on has a hand-written `BUILD.bazel` declaring eight targets, and a
  checked-in dep on `//.github/scripts` was dropped from
  `//infra/buildkite/governance` because `.github` is a dot-directory. The
  fabricated label now survives when the directory already holds a `BUILD` or
  `BUILD.bazel`, which is the only proof Bazel can load it; a dot-directory with
  no BUILD file -- `web/shared/public/.well-known` -- is still dropped.
