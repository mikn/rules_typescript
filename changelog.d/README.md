# changelog.d

One file per pull request. `CHANGELOG.md` carries only what has been released;
everything since is here, one entry per file, until a release folds them in.

`CHANGELOG.md` is a single place every parallel PR appends to, so every parallel
PR rebases on whichever one merged first — nine such rebases in one day, none of
them resolving anything but a union of two additions. Two fragments collide only
if they pick the same file name.

## Writing one

A fragment is Markdown. Its first line is the `###` section heading the entry
belongs under; everything after that is the entry, copied into `CHANGELOG.md`
verbatim — long-form prose, nested bullets, and code blocks included.

```md
### Fixed

- **A `tsconfig` `paths` value under the tsconfig's own directory no longer
  fails with `TS5090`.** The value is computed relative to the generated
  tsconfig, and a target below that directory relativised to a bare segment.
```

Name the file after the change rather than the PR number, lowercase with
dashes: `ts-binary-js-entry.md`, `jsonc-line-fidelity.md`.

## Sections

- `### Breaking — <area>` — pre-1.0 there is no deprecation window, so a break
  states the edit a consumer has to make. `### Breaking — ts_compile`.
- `### Added`, `### Changed`, `### Deprecated`, `### Removed`, `### Fixed`,
  `### Security` — [Keep a Changelog](https://keepachangelog.com/).
- Any other heading is allowed and sorts after those.

One section per file: a change that both breaks and adds gets two fragments.

The assembled order does not depend on when a fragment landed. `Breaking`
sections come first in alphabetical order, then the Keep a Changelog list above
in that order, then anything else alphabetically; within a section, entries go
in file-name order.

## Checking it

```bash
bazel run //tools/changelog                    # print the section as it will read
bazel test //tools/changelog:changelog_test    # parse every fragment here
```

A fragment with no heading, no entry under the heading, or two headings is an
error in both.

## Releasing

```bash
bazel run //tools/changelog -- --version 0.3.0 --write
```

writes the section above the newest release in `CHANGELOG.md` and deletes the
fragments it consumed. It is step 1 of
[the release process](../docs/RELEASE_PROCESS.md), before `//tools/release`
bumps and tags.
