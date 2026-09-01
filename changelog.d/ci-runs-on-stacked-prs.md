### Fixed

- **CI now runs on a pull request that targets another branch.** `ci.yml`'s
  `pull_request` trigger was filtered to `branches: [main]`, and its
  `on.pull_request` declares no `types`, so it defaulted to
  `opened, synchronize, reopened`. A PR opened against the branch below it in a
  stack therefore fired no workflow run at all -- not a skipped check, no run --
  and retargeting its base later did not help, because a base change is not one
  of those three events. PR #91 sat with "no checks reported" until its head was
  pushed again. The filter is gone; `push` stays scoped to `main` and `develop`
  so a stack's intermediate branches do not each trigger a second run.
