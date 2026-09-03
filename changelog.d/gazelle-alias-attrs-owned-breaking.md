### Breaking — gazelle

- **`path_aliases` on `ts_test`, and `path_alias_srcs` on `ts_compile` and
  `ts_test`, are Gazelle's now, so a hand-written value needs a `# keep`.**
  Gazelle writes both, and the run that wrote a value has to be the run that can
  correct it -- the reason `ts_config.deps` became mergeable. The edit: on every
  `ts_test` whose `path_aliases` you wrote by hand, and every `ts_compile` or
  `ts_test` whose `path_alias_srcs` you did, put `# keep` on the entry's own
  line to hold that entry beside what Gazelle computes, or above the attribute
  to hand the whole value back to you. Without one, `path_aliases` is recomputed
  entry by entry and each entry it drops is named in the log; `path_alias_srcs`
  is filled in after resolution, the way `deps` is, and a label dropped there
  goes without a report.
