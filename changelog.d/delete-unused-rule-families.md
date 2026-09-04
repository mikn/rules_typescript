### Breaking — rules

- **The rule families no consumer instantiated are gone: `ts_bundle`,
  `vite_bundler`, `ts_npm_publish` with `NpmPublishInfo`, `next_build`,
  `next_dev_server`, `next_serve`, `remix_build`, `svelte_library`,
  `sveltekit_build`, the oj dev server (`//oj:dev_server`), the Cloudflare
  Workers rules `ts_worker_deploy`, `ts_worker_dry_run`, `ts_worker_dry_run_test`
  and `ts_worker_types` with the launcher's `wrangler` mode, and in Gazelle the
  framework detection with its bundle, entry, Next.js and SvelteKit writers and
  the `remix` and `tanstack` packages.** A census of the Lovable monorepo's
  1,523 BUILD files found no instance of any of them, against 4,461
  `asset_library`, 1,355 `json_library`, 255 `ts_compile` and 78 `ts_test`
  calls; a rule nothing instantiates has no consumer whose breakage would fire
  a test, and every one of these carried its own wrapper, docs page, example
  workspace or nested-Bazel integration test. `ts/defs.bzl` exports 23 names
  where it exported 36, and Gazelle knows 14 kinds where it knew 19. A
  workspace loading one of the deleted names gets a load error naming it; the
  edit is to delete the target, since nothing left in the tree returns what it
  took -- except for `ts_worker_types`, whose replacement is the `ts_codegen`
  it expanded to, with the generator still shipped as
  `@rules_typescript//tools/codegen:wrangler_types`:

  ```python
  ts_codegen(
      name = "worker_types",
      srcs = ["wrangler.jsonc"],
      outs = ["worker-configuration.d.ts"],
      args = [
          "--config",
          "wrangler.jsonc",
          "--out",
          "{out}",
          "--srcs",
          "{srcs}",
          "--strict-vars=false",
      ],
      generator = "@rules_typescript//tools/codegen:wrangler_types",
      node_modules = ":node_modules",
      visibility = ["//visibility:public"],
  )
  ```

  `--config`, `--out` and `--srcs` are what the macro passed; whatever follows
  is the `wrangler types` command line as written. Gazelle now reads a
  `ts_codegen` whose `outs` names the file a tsconfig lists in
  `compilerOptions.types` as the `types_srcs` label for every target under
  that tsconfig, where it read a `ts_worker_types` target before; the pair it
  writes is unchanged. `ts_binary` keeps its `bundler` attr and `BundlerInfo`
  in both invocation modes, the generated-config one now passing four
  arguments (config, entry, output directory, stylesheet); `ts_dev_server`
  keeps `server`, defaulting to Vite, and `bundler`. `MODULE.bazel` declares
  neither the `oj_crates` hub nor the nightly Rust host tools, and the
  integration lane runs two CI legs (`npm`, `core`) over 13 nested-Bazel
  tests.
