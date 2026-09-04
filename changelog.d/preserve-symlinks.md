### Fixed

- **A type program holds its declared inputs and nothing the source tree has
  beside them.** The generated tsconfig sets `preserveSymlinks`. Bazel stages
  every input as a symlink into the source tree, and tsgo resolved a `types`
  entry to its realpath and that file's own imports from there: a
  wrangler-generated `worker-configuration.d.ts` holding
  `typeof import("./src/index")` pulled a worker's `src/` into a utils target
  that named none of it, and the target failed on a `TS2339` in a file it never
  declared. Read at the staged path the import resolves to nothing, and
  `skipLibCheck` drops the `TS2307` inside the `.d.ts` as it does for any
  declaration whose imports are not in the program. `preserveSymlinks` joins
  the keys `compiler_options` rejects.
