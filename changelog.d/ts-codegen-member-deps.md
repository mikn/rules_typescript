### Added

- **`ts_codegen` takes a workspace member's link target in `deps`.** A
  generator that resolves a member -- a Vite build whose entry imports
  `@example/design-system/fonts.css` -- names the importer's link target,
  `//<importer>:node_modules/<name>`, in `deps`; the link and the member's
  store tree join the action's inputs beside the importer's links, so the
  member resolves as it does from a file under the importer. `node_modules`
  stays the importer's directory and links. The rule had no attribute for a
  member's link, and the generator stopped at the import: `Rollup failed to
  resolve import "@example/design-system/fonts.css"`.
