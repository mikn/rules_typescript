### Fixed

- **The `declaration_type` example in the assets guide named the narrower
  element type.** `import("react").FC<import("react").SVGProps<SVGSVGElement>>`
  is what a `createLogo`-style consumer wants and what a project's own
  `declare module "*.svg"` usually does not say. Copied into a workspace whose
  ambient declares `SVGProps<SVGElement>`, it type-checks every import that
  goes through the generated declaration and fails the ones that still reach
  the ambient: `SVGProps` is invariant in its element, so
  `FC<SVGProps<SVGElement>>` satisfies a consumer expecting `SVGSVGElement`
  and the reverse does not. On one workspace the documented expression cleared
  43 errors and introduced 3. The example now uses `SVGElement` and says to
  match the project's existing ambient.
