### Added

- **Gazelle now names a cross-package import cycle at generation time,
  instead of leaving Bazel to print a loop of target labels.** Under the
  default `every-dir` package boundary each directory with `.ts` files is
  its own target, so two sibling directories importing each other are two
  targets importing each other, and a `deps` entry names the label, never
  the import behind it. The resolver records the graph over the targets this
  extension generates and reports each strongly connected component once,
  after the last import resolves: the packages in the cycle, and the targets
  that make it up. It names no import behind an edge and offers no remedy;
  either claim can be falsified by an attribute the computed value never
  reaches. Nothing is auto-resolved. Merging the cyclic directories would be
  `ts_package_boundary` applied without consent, and the cycle is genuine under
  per-target compilation even when every edge is `import type`: each target
  type-checks its own sources under `noEmitOnError`, where an `import type`
  resolving to nothing is a hard `TS2307`, and the emit itself needs nothing
  from the other side.

  An edge is an import a source of the emitted target writes, whose resolved
  label that target's emitted `deps` carry. A `srcs` or `deps` attribute
  Gazelle's own value cannot reach suppresses whatever its list leaves out: a
  `# keep` on the attribute or on the whole rule, and equally an existing
  expression the merger cannot reconcile value by value, which it logs and
  leaves untouched. A `# keep` on one `deps` value is not that, and a cycle the
  resolved labels close beside it is reported as any other. A dep no import
  explains is not an edge, so a cycle a hand-written label closes, the whole
  cycle or only its last edge, is left to Bazel, whose loop of labels names
  the BUILD file that label is written in. A cycle inside a single directory
  -- the doc-target and test-target splits -- is not covered, and neither is a
  cycle through a `ts_test` dep that no source of its own imports.
