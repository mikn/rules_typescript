### Changed

- **The persistent root is now caches — plus, outside `bazel test`, fallback run
  roots — rather than per-test trees.** `$RULES_TS_IT_SCRATCH` keeps its name for
  the CI job that sets it, but the repository cache and the disk cache are what
  it exists for: content-addressed, so a stale entry is a miss and never a wrong
  answer, and shareable across checkouts for that reason.
- **The retained output base moved rather than disappeared, and is now
  short-lived.** A finished run still keeps its nested output base — deleting
  gigabytes at the end of every test would cost more than it saves. Under
  `bazel test` it is retained in `$(bazel info
  output_base)/execroot/_main/_tmp/`, which is less discoverable than the old
  `~/.cache/rules_typescript_it/<test>/output_base` but not long-lived: the
  outer Bazel clears that whole directory on each `bazel test`, whatever the
  target. Measured — a full local suite left 6.8G across 19 nested output bases
  there, and the next `bazel test`, of one unrelated target, left only that
  target's 264K.
- **The old layout is yours to clean up, and it is still live.** Nothing in this
  change deletes it, and every checkout that has not picked this up keeps
  writing there. On the machine this was developed on, the persistent root held
  8.7G across 19 per-test directories beside 7.1G of caches, 16G in all — and
  while that was being measured another checkout on the old code added a
  twentieth, for a test name this branch does not have. So it is a cleanup to do
  when nothing is running the suite, not one to schedule: deleting one of those
  directories under a live run is the exact failure this change fixes. To list
  them:

  ```bash
  root="${RULES_TS_IT_SCRATCH:-${XDG_CACHE_HOME:-$HOME/.cache}/rules_typescript_it}"
  find "$root" -mindepth 1 -maxdepth 1 -type d \
    ! -name repository_cache ! -name disk_cache ! -name runs -print
  ```

  Re-run with `-exec rm -rf {} +` once the list looks right and no checkout is
  mid-suite.
- **Cost.** A local run pays for the output base it no longer inherits: a flat
  ~13.5s per test (five interleaved pairs — `new_project_test` 27.6s retained
  against 41.1s fresh, `npm_deps_test` 28.7s against 42.5s), which is analysis
  and repo setup and not a re-fetch, because the content-addressed caches are
  what make a warm run warm. CI pays nothing: it provisions `/mnt/rules_ts_it`
  with a bare `mkdir -p` on a fresh runner and its cache step restores only
  `repository_cache` and `disk_cache`, never the per-test output bases, so every
  nested output base was already being created empty on every CI run.
