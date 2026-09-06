### Changed

- **An npm package is extracted under `node_modules/<name>/` inside its
  repository, not at the repository root.** Every path the rules write for a
  package gains the segment: the `paths` values in a generated tsconfig, the
  action inputs, the exec paths
  (`external/+npm+npm__zod__4_1_5/node_modules/zod/index.d.ts`), and the labels
  in the generated BUILD file (`package_dir`, `exports_types`, `exports_files`).
  TypeScript classifies a `paths` match by that segment: a file under
  `node_modules` is a library file, type-checked but never emitted and outside
  the `rootDir` check, and a file under none is project source, so a `.ts`
  reached through `paths` is `TS6059` the moment a package's module entry is
  one. A test or script that matched an exec path by repository name alone has
  to expect `node_modules/<name>/` after it. The `node_modules` tree and the
  editor's `.bazel/npm/<name>/` copies are laid out from the package root, as
  before; the tree no longer carries the generated `BUILD.bazel` and
  `REPO.bazel` of each package, which the root glob had matched.
