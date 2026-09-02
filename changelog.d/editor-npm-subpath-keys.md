### Fixed

- **The editor's tsconfig now gives an npm `exports` subpath its own `paths`
  key, as the build's already did.** `ts_compile` reads
  `NpmPackageInfo.subpath_types` and writes one key per subpath;
  `tsconfig_aspect` wrote only the bare package key and a `<name>/*` wildcard
  beside it. The wildcard is a guess: it substitutes the subpath as a path
  under the package and probes `.ts`, `.d.ts` and `/index.d.ts`, so it finds
  `postcss/lib/node.d.ts` and finds nothing at all for
  `rolldown/dist/config.d.mts`. Over `//tests/npm_types_barename:mangled_scope`'s
  closure 41 of the build's keys were subpaths the editor had no key for, and
  14 of those resolved to nothing on the editor side while the build resolved
  them -- `import { defineConfig } from "rolldown/config"` was `TS2307` in the
  editor and clean in the build. On this repo's own checked-in `tsconfig.json`
  the change adds 141 keys: 37 were `TS2307`, and of the 104 the wildcard
  already answered, 31 answered with a file the package manifest does not
  designate.

  A subpath key takes no `<key>/*` companion of its own, which is the
  `wildcard` field `ts_compile`'s `npm_types_aliases` structs already carried.
