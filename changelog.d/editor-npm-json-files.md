### Fixed

- **`bazel run //:refresh_tsconfig` now installs the `.json` an npm package
  publishes, not only its declarations.** The root config already writes a
  `<pkg>/*` key into `.bazel/npm/<pkg>`, and the editor filled that directory
  from `declaration_files` plus the manifest alone. `import tags from
  "lucide-static/tags.json"` was a key naming a file nobody had put there,
  while `ts_compile` stages the same set into its build sandbox.

  It is the whole `.json` set, as in the sandbox; the rule does not read which
  paths a package's manifest designates. Nested `package.json` files come with
  it, 22 of the 54 files added over this repo's own closure. A nested manifest
  is the nearest one whose `type` a staged `.d.ts` inherits, and it decides
  what a directory-shaped `<pkg>/*` match resolves to. Over 865 specifiers
  against the checked-in config, that resolves three more
  (`blake3-wasm/dist/wasm/{browser,nodejs,web}`) and regresses none. The
  staged tree under `.bazel/npm` goes from 2327 files / 14,831,715 bytes to
  2381 / 22,912,369. 7.67 MB of that is two packages' unimported data:
  `wrangler`'s 3,221,172-byte esbuild metafile and `typescript`'s 13 localised
  `diagnosticMessages.generated.json` (4,453,200 bytes together).
