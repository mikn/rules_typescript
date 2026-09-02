### Added

- **Gazelle now names a cross-package import cycle at generation time, instead
  of leaving Bazel to print a loop of target labels.** Under the default
  `every-dir` package boundary each directory with `.ts` files is its own
  target, so two sibling directories importing each other are two targets
  importing each other -- and a `deps` entry names the label, never the import
  behind it. The resolver records the graph over the targets this extension
  generates and reports each strongly connected component once, after the last
  import resolves: the packages in the cycle, and the targets that make it up.
  The message stops there. It names no import behind an edge and offers no
  remedy, because either claim is falsified by a `# keep` -- on `srcs`, on
  `deps`, or on the whole rule -- and a report that cannot be wrong is worth
  more than one that is usually right. Nothing is auto-resolved either: merging
  the cyclic directories would be `ts_package_boundary` applied without consent,
  and the cycle is genuine under per-target compilation even when every edge is
  `import type` -- each target is its own compiler invocation, and emitting one
  side's `.d.ts` needs the other's. An edge is one thing: an import a source of
  the emitted target writes, whose resolved label that target's emitted `deps`
  carry. So a `srcs` or `deps` attribute -- or a rule -- held with `# keep`
  suppresses whatever its list leaves out, since the merger discards what
  Gazelle computed there; a `# keep` on one `deps` value is not that, and a
  cycle the resolved labels close beside it is reported as any other. A dep no
  import explains is not an edge either, so a cycle a hand-written label closes
  -- the whole cycle or only its last edge -- is left to Bazel, whose loop of
  labels names the very BUILD file that label is written in. Of the cycles
  inside a single directory, only the framework entry split is reported -- by
  the framework-entry report, which can name the `entry_point` behind it. The
  doc-target and test-target splits are not covered, and neither is a cycle
  through a `ts_test` dep that no source of its own imports.
