### Added

- **`ts_test` now takes `types_srcs`,** forwarded to the `ts_compile` it
  generates for the test sources. `ts_compile` accepts it and the `ts_test`
  macro's signature did not, so a `types_srcs` on a test target was
  `ts_test() got unexpected keyword argument: types_srcs` — a load error that
  fails `bazel query` on the whole package, not just the target that carries
  it:

  ```python
  ts_test(
      name = "handler_test",
      srcs = ["handler.test.ts"],
      types = ["../worker-configuration.d.ts"],
      types_srcs = ["//worker:worker_types"],
      deps = [":src", "@npm//:vitest"],
  )
  ```

  The attribute carries the meaning it carries on `ts_compile`, and a test
  target needs it more often than a library does: the generated compile's only
  `srcs` are the test files, so unless a dep already stages the declaration —
  a `.d.ts` in a dep's own `srcs` is passed through and sits at the path the
  entry names — `types_srcs` is what does. See
  [ts_test](https://mikn.github.io/rules_typescript/rules/ts-test/#a-types-entry-that-names-a-declaration-file).
