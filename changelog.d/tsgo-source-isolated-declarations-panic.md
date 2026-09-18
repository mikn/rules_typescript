### Fixed

- **The source-built compiler reports `TS9013` where TypeScript 7.0.2 crashes
  under `isolatedDeclarations`.** An exported initializer of the shape
  `f().p || ""` made `tsc` exit 2 with `panic: Unhandled case in Node.Text:
  *ast.CallExpression`: the declaration tracker's `isBoundExpando` took every
  binary expression with a property access on its left for an expando
  assignment and asked the resolver about the head of the access chain.
  `ts/private/tsgo_source/isolated-declarations-bound-expando.patch`, applied
  to the module `//ts/toolchain/tsgo_source` builds, asks only when the
  operator is `=` and the head is an identifier, and carries the module's own
  test case with its baselines. The lockfile toolchains' binary keeps the
  crash; a build that meets the shape under `--//ts:declarations=oxc`
  registers `@rules_typescript//ts/toolchain/tsgo_source` ahead of
  `//ts/toolchain:all`.
