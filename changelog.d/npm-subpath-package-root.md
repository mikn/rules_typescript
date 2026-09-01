### Fixed

- **A subpath of an npm package with no `exports` map now resolves under the
  package root.** The `pkg/*` wildcard hung off whichever directory the
  package's root declaration landed in, so `recharts/types/shape/Curve` was
  looked up as `<recharts>/types/types/shape/Curve` and reported `TS2307`.
  npm reads a subpath as a plain path under the package root whenever the
  manifest declares no `exports`, and most of the registry declares none.
  The wildcard now lists the package root first and keeps the entry
  directory behind it, so a package whose subpaths do sit beside its entry
  resolves exactly as before. A subpath the manifest names outright is
  unaffected: an exact `paths` key still beats the pattern.
