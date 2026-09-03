### Fixed

- **An ambiently declared module gets no dep.** A script-mode `.d.ts` holding
  `declare module "mobile"` is that module: nothing installs it and no other
  file exports it, so the specifier naming it needs no dep. Gazelle did not
  read the construct; its scanner only refused to read the declaration's own
  string as an import. `import type { AuthResult } from "mobile"` fell down
  the bare-specifier ladder to the hub convention and came back as
  `@npm//:mobile`, a target no hub declares. Bazel answers that with
  `no such target` during analysis, which fails every target in the build.
  The names a target's own declaration files declare are now read, and a
  specifier one of them covers resolves to nothing. Only that target is exempt:
  a sibling package importing the same name still asks the hub. An installed
  package keeps its dep when a declaration file names it, since the lockfile
  says a hub target exists.
