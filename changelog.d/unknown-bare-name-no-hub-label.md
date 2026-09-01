### Fixed

- **A bare specifier the workspace never installed no longer resolves to an npm
  hub label.** The hub is built from `pnpm-lock.yaml`, so a name that lockfile
  never mentions — a package a nested `package-lock.json` or `bun.lock`
  installed, a `@/...` alias no `tsconfig` in scope expands — can only produce a
  target that does not exist, and Bazel answers `no such target` by failing
  analysis for every target in the build. Gazelle now writes no dep for such a
  name, so the compiler reports one `TS2307` on the import instead.

  The refusal reads every name the lockfile mentions, not the npm inventory: the
  inventory drops packages carrying `os:`/`cpu:`/`libc:` rather than duplicate
  `platforms.bzl` in Go, and gating on it would turn that deliberate under-claim
  into a dropped dep. A tree with its own `# gazelle:ts_npm_hub` resolves against
  a second lockfile this reader never saw, so nothing there is refused, and a
  workspace with no root lockfile at all keeps every label it had.
