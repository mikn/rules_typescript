### Changed

- **A changelog entry is now a file in `changelog.d/`, not an edit to
  `CHANGELOG.md`.** One file per pull request: the first line is the `###`
  section it belongs under, the rest is the entry, and the whole file is copied
  into `CHANGELOG.md` verbatim at release time by
  `bazel run //tools/changelog -- --version X.Y.Z --write`. `CHANGELOG.md` was
  the one place every parallel PR appended to, so every parallel PR rebased on
  whichever one merged first. Nothing already in `CHANGELOG.md` moves, and the
  section headings are unchanged; `changelog.d/README.md` is the format.
