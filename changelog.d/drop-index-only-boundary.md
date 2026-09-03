### Changed

- **`# gazelle:ts_package_boundary true` is documented for the mode that reads
  it.** The reference called it "useful in index-only mode without
  `index.ts`", and that was wrong about which mode needs it: generation reads
  the flag in the `tsconfig` arm too, where marking the directory is the only
  way to make a package of one the covering `tsconfig.json` does not sit in —
  which is what the diagnostics about a boundary swallowing a directory's
  targets already advise. The value is unchanged; only its documentation is.

- **One fewer refusal on the `tsconfig` attribute.** A target named no baseline
  when the directory holding the `tsconfig.json` would have to become a package
  to hold the `ts_config`, on the grounds that a BUILD file written there stops
  the roll-up walk and drops every source beneath it. With `index-only` gone the
  case cannot arise: in `tsconfig` mode a directory holding a `tsconfig.json`
  *is* a package, and under `every-dir` the roll-up walk does not run. The
  remaining refusals — a `ts_ignore`, a framework's staging glob, a boundary
  directive declared between the two directories, and a target already named
  `tsconfig` — are unchanged.
