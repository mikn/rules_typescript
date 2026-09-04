### Fixed

- **A `ts_compile` dep's npm packages now get `paths` keys in the consumer's
  program.** The map was built from the deps carrying `NpmPackageInfo` and
  their own closures; a `ts_compile` dep contributed its declarations and
  nothing about the packages they import. `TsDeclarationInfo` now carries the
  npm closure beside the declarations, and a consumer names every package in
  it. In the Lovable monorepo `//packages/ui:ui_doc` depends on
  `//packages/ui:ui`, whose `kbd.d.ts` imports `VariantProps` from
  `class-variance-authority`, a package only `:ui` declared. The staged
  declaration resolved the import to nothing, `skipLibCheck` hid the `TS2307`,
  `interface KbdProps extends VariantProps<...>` kept only its own members, and
  56 doc sites reported `TS2322` on props the component declares. A package
  still needs a direct dep before this target's own sources may import it: the
  closure feeds `paths` and the "add this dep" answer, not the declared set.
