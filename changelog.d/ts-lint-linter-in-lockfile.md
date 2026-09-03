### Fixed

- **A linter config whose linter `pnpm-lock.yaml` never mentions no longer
  produces a `ts_lint`.** An `eslint.config.mjs` on disk was enough for a
  `ts_lint` naming `@npm//:eslint_bin`, a target the hub does not declare when no
  eslint was installed — a config left behind by a package that never was, or
  the live config of a nested `package-lock.json` island — and Bazel answers
  `no such target` by failing analysis for every target in the package. The
  linter binary now takes the same test a bare specifier does: a name the
  lockfile never mentions gets no hub label. Gazelle writes no `ts_lint` for the
  directories that config covers, withdraws one an earlier run wrote, and says
  so once per config file, naming the config, the package, and the label.

  The `linter_binary` also follows the tree's `# gazelle:ts_npm_hub`, the way
  a bare import's dep does: `@npm_eslint//:eslint_bin` under
  `# gazelle:ts_npm_hub npm_eslint`, where it used to name `@npm` regardless.
  The `ts_codegen` generator, `vite_bundler`, framework `node_modules` and
  tsconfig `types` labels do not follow it yet and still name `@npm`.
  A tree under its own hub resolves against a lockfile this reader never saw,
  so nothing there is refused, and a workspace with no root lockfile keeps
  every `ts_lint` it had.
