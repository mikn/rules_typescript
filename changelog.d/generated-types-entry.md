### Changed

- **A relative `compilerOptions.types` entry now resolves to a generated
  declaration.** The entry was rebased onto the source tree whatever it named,
  so a `.d.ts` a rule generated -- staged by `types_srcs`, by `srcs`, or on a
  dep edge -- sat in `bazel-out` at the path the entry named and was refused at
  analysis with a message pointing at `public_globals`. The entry is now written
  into the generated config as the path to the file it resolved to, wherever
  the label put it, and the two refusals are gone. Measured on the
  `worker-configuration.d.ts` fixture: the same target fails at analysis on
  the previous rule and type-checks on this one, with the entry written
  `../worker-configuration.d.ts` from a generated config one directory below
  the generated file.
