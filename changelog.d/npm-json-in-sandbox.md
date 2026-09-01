### Fixed

- **A `.json` file an npm package publishes is now importable.** `paths`
  resolved `import tags from "lucide-static/tags.json"` to the right file and
  the compile action never carried it: `ts_compile` stages a package's
  declarations and its `package.json` and nothing else, so `resolveJsonModule`
  found nothing at a path that was correct. `NpmPackageInfo` now carries the
  package's `.json` files and `ts_compile` puts them in the sandbox, across
  the same flattened closure `paths` is written from.
