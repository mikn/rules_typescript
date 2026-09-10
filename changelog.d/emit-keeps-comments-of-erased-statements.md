### Fixed

- **A comment above a statement the transform erases stays in the emit.**
  oxc drops a statement's leading comments with the statement, so a file
  opening with `// @vitest-environment node` over an `import type` compiled
  to a file with no docblock, and vitest -- which reads the docblock from the
  file it runs, under Bazel the compiled sibling -- ran it under the config's
  environment. oxc's emit moves such a comment to the next top-level
  statement the source keeps, or to the file's end when none follows.
  `//tests/vitest/docblock` pins it.
