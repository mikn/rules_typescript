### Fixed

- **Two checkouts of this repo can run the same integration test at the same
  time.** The harness keyed the child workspace, the scratch dir and the nested
  output base on the test's name alone, under one persistent root
  (`$RULES_TS_IT_SCRATCH`, else `$XDG_CACHE_HOME`, else `~/.cache`), so every
  checkout on a machine staged into the same directory, and each one's
  `prepare()` deleted the other's tree mid-run. The failure never named the
  cause: one checkout reported `could not download Bazel: ... no such file or
  directory`, another `FATAL: getcwd() failed`, both against a path the run
  itself had created. Under `bazel test` those three now live in the test's
  `TEST_TMPDIR`, `<outer output base>/execroot/_main/_tmp/<per-target hash>`,
  so two targets differ in the hash and two checkouts differ in the output base
  in front of it. Invoked outside `bazel test` there is no `TEST_TMPDIR`, and
  the run root falls back to `<persistent root>/runs/<hash of the
  checkout>/<test name>`, the same separation, on the same disk as the caches.
- **A killed integration test no longer strands its output base under a name
  nothing looks for.** `prepare()` reset the workspace and the scratch dir but
  only `MkdirAll`ed the output base. Under `bazel test` the output base is now
  inside `TEST_TMPDIR`, and the outer Bazel clears the whole `_tmp` it lives in
  on each `bazel test`, so an interrupted run leaves nothing that outlives the
  next invocation. On the fallback path the run root is stable per checkout and
  per test, so a killed run's output base is the one the next run of that test
  reuses and overwrites in place: one per test per checkout, the bound the old
  layout had, minus the cross-checkout collision.
