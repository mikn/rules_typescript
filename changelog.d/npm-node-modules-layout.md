### Changed

- **An npm package is extracted under `node_modules/<name>/` inside its
  repository, not at the repository root.** Every path the rules write for a
  package gains the segment: the action inputs, the exec paths
  (`external/+npm+npm__zod__4_1_5/node_modules/zod/index.d.ts`), and the labels
  in the generated BUILD file (`package_dir`, `exports_files`). TypeScript
  classifies a file by that segment: one under `node_modules` is a library file,
  type-checked but never emitted and outside the `rootDir` check, and one under
  none is project source. A test or script that matched an exec path by
  repository name alone has to expect `node_modules/<name>/` after it. The
  `node_modules` tree is laid out from the package root, as before; it no longer
  carries the generated `BUILD.bazel` and `REPO.bazel` of each package, which
  the root glob had matched.
