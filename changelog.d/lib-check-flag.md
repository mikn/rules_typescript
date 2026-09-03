### Added

- **`--//ts:lib_check` turns `skipLibCheck` off for every target in the build.**
  The baseline sets `skipLibCheck: true`, under which a third-party `.d.ts`
  whose own imports do not resolve widens to `any` in silence. That hid the
  `@types/*` mapping fixed in this release and, before it, an unresolvable
  `asset_library` `declaration_type`; both were found by hand-editing
  `compiler_options` on one target. The flag applies the same thing over
  whatever `compiler_options` or a named `tsconfig` say:

  ```bash
  bazel build //... --//ts:lib_check
  ```

  It is a diagnostic sweep, not a mode to build in: it also reports what a
  dependency needs from `lib` and the program does not set.
