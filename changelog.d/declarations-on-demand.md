### Breaking — ts_compile

- **A target's `.d.ts` leave its default outputs.** `bazel build //pkg:lib`
  runs `TsgoCheck`, a `--noEmit` validation over a tsconfig that turns the
  declaration emit off, and writes no `lib.d.ts`; `TsgoDeclare` emits the
  declarations, with `--declaration --emitDeclarationOnly --noEmitOnError` on
  its command line, when a dependent's compile reads them
  (`TsInfo.declarations`) or the `declarations` output group is requested --
  `bazel build //pkg:lib --output_groups=declarations`, or a
  `filegroup(output_group = "declarations")`. A `data` entry or filegroup that
  read a `ts_compile`'s default outputs for its `.d.ts` names the group
  instead. The check runs no declaration transformer, so `TS4xxx` declaration
  diagnostics surface in the declare; a chain that sets `isolatedDeclarations`,
  or `--//ts:declarations=oxc`, keeps the `declaration` that option requires,
  and the check reports them there. `--norun_validations` now skips the type
  check in both modes; `--@rules_typescript//ts:lint=@rules_typescript//ts:no_lint`
  turns the linter off alone. `tsaction tsconfig` loses `-emit`, `-out_dir`,
  `-root_dir` and `-declaration_map`; `tsaction tsgo` runs without `-check`.
  A workspace member's hub view `@npm//:<name>` has the package's files as its
  default outputs, the `.d.ts` included.
