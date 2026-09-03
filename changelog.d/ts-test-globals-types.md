### Fixed

- **`ts_test`'s `globals` now supplies the declarations as well as the runtime
  globals.** The attribute appended `globals: true` to the generated vitest
  config and stopped there, so `describe`, `it` and `expect` existed when the
  test ran and were unknown to the type program that had to compile it first:
  `TS2593` on `describe` and `it`, `TS2304` on `expect` and the hooks. Every
  `globals = True` target therefore needed a hand-written `.d.ts` redeclaring
  vitest's own globals — this repo's `//tests/vitest/attrs` carried one, which
  is why the attribute looked covered. `globals = True` now also adds
  `"vitest/globals"`, the subpath vitest publishes those declarations behind, to
  the `types` of the `ts_compile` it generates, and the shims can go.

  A `types` entry resolves from the target's own deps, so `globals = True`
  requires vitest among them and says so at analysis time when it is missing.
  That is not the dep the runner needs — it finds vitest in the `node_modules`
  tree — so a `globals = True` test supplying vitest through the `node_modules`
  attr alone analysed before this change and now has to name it in `deps` too.
  Under Gazelle
  the dep needs `# keep`: nothing in such a test imports vitest, and `deps` is a
  managed attribute. See
  [ts_test](https://mikn.github.io/rules_typescript/rules/ts-test/#globals).
