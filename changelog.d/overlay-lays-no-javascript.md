### Fixed

- **The program root lays a dep's declarations, manifest and data over its
  package, and none of its JavaScript.** A first-party dep at or above the
  target's package had its whole output directory laid over the package's
  sources; with a JavaScript src in the target (`allowJs`), a dep's `.js`
  under the tsconfig's pattern -- a `ts_codegen` tree of `.js` beside `.d.ts`
  -- was a root of the program, and `TsgoDeclare` wrote a `.d.ts` for each
  onto the dep's own (`TS5033`). A program reads a dep through its
  declarations; its JavaScript is a runfile.
