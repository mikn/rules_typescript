### Fixed

- **A subpath an npm package's `exports` maps through a one-star pattern now
  resolves where the manifest maps it.** `paths` gave every package a `pkg/*`
  wildcard over the package root and the entry's directory, and the fetch
  dropped every `exports` key or target holding a star, so a package mapping
  `./*` to `./dist/esm/*` had no key reaching a declaration only that pattern
  maps: `@modelcontextprotocol/sdk` names seven exact subpaths and reaches
  `./server/mcp.js` through `./*` alone, so
  `import "@modelcontextprotocol/sdk/server/mcp.js"` was `TS2307`, and every
  handler typed through `McpServer` widened to `any` behind it. The pattern is
  now read where the manifest is, the first condition in the map's own order
  whose target fits one star, and written as the first value of the
  `pkg/<subpath>` key with the star and any suffix kept (`./utils/*` →
  `dist/types/utils/*.d.ts`; a starred key whose target has no star resolves
  every match to that one file), the two guesses behind it, in the build's
  tsconfig and the editor's alike. A key with two stars or a `null` target is
  unchanged.
