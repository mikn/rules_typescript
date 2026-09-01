### Added

- **`--//ts:lib_check` turns `skipLibCheck` off for every target in the build.**
  The baseline sets `skipLibCheck: true`, which is what makes a third-party
  `.d.ts` whose own imports do not resolve a silent widening to `any` rather
  than an error -- the class of bug that hid the `@types/*` mapping above and,
  before it, an unresolvable `asset_library` `declaration_type`. Both were found
  by hand-editing `compiler_options` on one target; this is the same thing as a
  flag, applied over whatever `compiler_options` or a named `tsconfig` say:

  ```bash
  bazel build //... --//ts:lib_check
  ```

  A diagnostic sweep, not a mode to build in: it also reports what a dependency
  needs from `lib` and the program does not set.
