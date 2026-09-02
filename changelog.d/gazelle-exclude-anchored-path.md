### Added

- **`# gazelle:ts_exclude` takes a path-anchored pattern, written with a leading
  `./`.** A bare pattern is matched against the basename, so it drops a file of
  that name at every depth below the declaration and a package-root file could
  not be named at all: `vite.config.ts` in `web/BUILD.bazel` meant that name
  anywhere under `web/`, including a `web/**/vite.config.ts` nobody had written
  yet. `# gazelle:ts_exclude ./vite.config.ts` resolves the rest of the pattern
  against the directory whose build file declares it and matches the path, so it
  drops `web/vite.config.ts` and leaves the namesake below it alone. The path
  may be any depth (`./plugins/one.ts`), and a `*` does not cross a `/`, so
  `./*.gen.ts` covers the declaring directory's own files and no
  subdirectory's. Bare patterns are unchanged: a name still matches at every
  depth, and a bare pattern carrying a `/` still matches the path a rolled-up
  file was reached by.
