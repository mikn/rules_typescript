### Fixed

- **A store package's import of a name it does not declare resolves through
  pnpm's hidden hoist, at run time and in the compile.** `ts_compile` and
  `ts_test` stage, beside the chain's links, the hoist links whose names the
  target's npm closure holds, with the store trees they enter -- pnpm's pick
  for a name can be a snapshot no edge of the closure reaches -- so a
  `Cannot find module` from a third-party package's undeclared `require`
  (grpc-gcp's `protobufjs/minimal`, with `@grpc/grpc-js` its one declared
  dependency and `protobufjs` declared by `@google-cloud/spanner` beside it)
  is gone, and the action's inputs stay the closure's names: a bump re-runs
  the targets whose closure holds the package. A first-party source importing
  an undeclared name still fails the ownership check.
