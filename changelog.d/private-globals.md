### Added

- **`ts_compile(private_globals = [...])` keeps an ambient `.d.ts` private to
  its own compilation.** A `.d.ts` with no top-level import or export declares
  globals, and until now those reached every consumer with no way to opt out.
  That is right about TypeScript and not always right about packaging: a
  package can hold an ambient it needs for its own standalone `tsc -p` that is
  no part of its public type surface -- a `process` shim in a library with no
  `@types/node`, which then shadows the real `process` in every consumer that
  has it. A src named here still types the target that owns it and is left out
  of the generated `<name>.globals.d.ts` consumers list in `files`, the only
  route its declarations had into their programs. Every entry must be in
  `srcs`, and must be global: naming a module-scoped `.d.ts` fails the build
  rather than passing as a no-op.
