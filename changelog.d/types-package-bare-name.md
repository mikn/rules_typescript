### Fixed

- **A `@types/x` npm dep now answers an import of `x`.** DefinitelyTyped ships
  `x`'s declarations as `@types/x` (and a scoped `@a/b`'s as `@types/a__b`), and
  TypeScript pairs the two by walking `node_modules/@types`. There is no
  `node_modules` here -- npm packages reach the compiler through `paths` -- so
  the package was in the program under a name nothing imports. A `@types/*` dep
  now gets its `paths` entries twice, once under its own name and once under the
  name it types.

  The failure was mostly not on anyone's own import line: the specifiers that
  need the mapping are inside *other packages'* declarations, where
  `skipLibCheck` hides the `TS2307` and what those declarations export widens to
  `any`. In this repo's lockfile, `rollup`'s `dist/rollup.d.ts` opens with
  `import type * as estree from 'estree'`, `@types/estree` has no runtime
  package to be reached under, and the first visible symptom was a `TS7006` on a
  callback parameter in application code five packages away.

  Which of the two names wins follows npm: the runtime package answers `x` when
  it publishes declarations of its own, `@types/x` when it publishes none
  (`@babel/core` ships no `.d.ts`; `@babel/types` does), and a `path_aliases`
  prefix outranks both.
