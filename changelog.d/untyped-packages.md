### Added

- **`ts_compile` takes `untyped_packages`, a per-target list of npm packages to
  keep out of that target's type program.** A named package gets no `paths` key
  (not its own, not one per `exports` subpath, and not the bare name a
  `@types/*` package would answer for it) and no `files` entry. The dep stays
  in `deps`, its files stay among the action's inputs, and no JavaScript moves.
  Strict deps does see it: a direct dep stays declared, while a package that
  was only reachable leaves the reachable set with the key, so an import of it
  is a bare `TS2307`, not "add this dep"; adding it back would not type it.

  An entry names one package, and a package's declarations live wherever npm
  put them: `ms` ships none and is typed by `@types/ms`, so `["@types/ms"]` is
  the entry that takes those declarations out, after which `ms` resolves to
  the runtime package it names. `["ms"]` takes away the bare name `@types/ms`
  answered for it. Name both to leave nothing.

  The case is a package whose declarations are a global script, a .d.ts with
  no top-level import or export. Everything such a file declares belongs to
  every program the file is part of, and a dynamic `import()` loads a module's
  declarations exactly like a static one. In the Lovable monorepo, one
  `void import("@sentry/cloudflare")` inside an `import.meta.env.SSR` branch of
  a browser component reached `@cloudflare/workers-types` through that
  package's own declarations, whose `interface Element` and `interface Body`
  then merged into lib.dom for the whole target: 42 errors across 21 files that
  name neither package, none of them near the import.

  ```python
  ts_compile(
      name = "web",
      srcs = glob(["src/**/*.ts", "src/**/*.tsx"]),
      untyped_packages = ["@cloudflare/workers-types"],
      deps = ["@npm//:sentry_cloudflare"],
  )
  ```

  The example needs no `declare module`: it names the global-script package one
  hop behind the import, so `import("@sentry/cloudflare")` still resolves and
  the unresolvable import inside Sentry's own declarations is covered by
  `skipLibCheck`. A target that imports an excluded package itself is the other
  case. The import resolves to nothing, which is `TS2307`, so it needs a
  `declare module "<name>"` in a .d.ts src to say what the import means there.
  That declaration answers only because the `paths` key is gone: with the key
  in place TypeScript resolves the specifier and adds the file to the program
  before the checker ever asks about an ambient module, and the globals arrive
  anyway (`//tests/untyped_packages:shim_only` is that half).

  The attribute is per target and travels through no dep edge, so a dependent
  that needs the package resolves it as before. The editor is workspace-wide:
  the tsconfig `bazel run //:refresh_tsconfig` writes has one `paths` map, and
  a nested tsconfig extends the root and inherits it unchanged. Where every
  target reaching a package excludes it, the editor drops it with no further
  configuration. Where one target excludes a package another still resolves,
  one map cannot answer both ways, and `ts_refresh_tsconfig` fails naming the
  target, the package, and `host_only_packages` as the one place a
  workspace-wide answer fits.

  Two mistakes are refused: an entry naming no package in the target's
  closure, which would keep nothing out, and a package named in both
  `untyped_packages` and `compilerOptions.types`, which ask for opposite
  things.
