### Fixed

- **A `ts_compile` dep's npm packages are now in the `ts_test` runtime tree.**
  The auto-generated `node_modules` linked the deps carrying `NpmPackageInfo`
  and their closures; a `ts_compile` dep put its compiled JS in the runfiles and
  nothing about the packages that JS imports, so a test in one package running
  production code from another failed at import time with `ERR_MODULE_NOT_FOUND`
  for every package only the production target declared. In the Lovable
  monorepo `//workers/entri-webhook/test:test_test` loaded 5 of its 15 suites
  red on `web-auth-library`, `@lovable/custom-domain-utils` and
  `@flarelabs-net/workers-observability-utils/metrics`, all three declared by
  `//workers/entri-webhook/src` alone. The tree now follows the closure
  `TsDeclarationInfo.transitive_npm_packages` already carries for `paths`: the
  test's own npm deps first, then each other dep's closure, one entry per
  resolution, and the test's own dep stays the resolution that sits flat where
  a name resolved more than one way.
