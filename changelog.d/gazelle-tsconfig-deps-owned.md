### Breaking — gazelle

- **`ts_config.deps` is Gazelle's, so a hand-written value there now needs a
  `# keep`.** Gazelle writes the `extends` chain into that attribute, and the
  run that wrote a label is the run that corrects it. While `deps` was
  write-once, deleting or moving the base `tsconfig.json` left
  `deps = ["//workers/proxy:tsconfig"]` naming a target no package declares,
  and no later run cleared it. One label nothing satisfies fails analysis for
  the whole workspace, not just the target that named it.

  The edit: on every `ts_config` whose `deps` you wrote by hand, add a `# keep`
  above the attribute, which keeps the whole value, or on an element's own
  line, which holds that entry and lets Gazelle's computed label join it.
  Without one the next run replaces the value and names, in its log, each entry
  it dropped. An `extends` shape Gazelle does not read (an array, a
  package-form specifier, an absolute path) computes no label, so there a
  `# keep` is the only thing between a hand-written chain and an empty `deps`.
