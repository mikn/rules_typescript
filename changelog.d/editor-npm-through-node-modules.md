### Breaking — ts_refresh_tsconfig

- **The editor tsconfig names no npm package; npm resolves through the
  checkout's node_modules.** `bazel run //:refresh_tsconfig` wrote one `paths`
  key per npm package, pointing into `.bazel/npm/<name>` copies of its
  declarations, and named every `@types/*` entry point in `files`; both routes
  are gone, with the `npm_dir` and `host_only_packages` parameters that steered
  them, and the tsserver hook's map holds first-party packages only. TypeScript
  walks the checkout's node_modules from the importing file, as tsgo walks the
  importer chain a build stages. Run `pnpm install` so that tree holds what the
  lockfile resolves, drop the two parameters from `ts_refresh_tsconfig`, and
  delete `.bazel/npm`.
