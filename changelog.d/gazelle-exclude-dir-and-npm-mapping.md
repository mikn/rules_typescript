### Added

- **`# gazelle:ts_exclude_dir <basename>` keeps Gazelle out of a directory
  entirely.** `# gazelle:ts_ignore` suppresses generation for the directory
  whose build file carries it, which is no help for the directories one
  actually wants skipped: `coverage/`, `storybook-static/` and the rest have no
  build file to put a directive in, and writing one there to say "ignore me" is
  backwards. This directive is declared in an **ancestor** and names a
  basename, so nothing is generated in any directory of that name below it, on
  top of the built-in `.next`, `.nuxt`, `.svelte-kit`, `dist` and `build`.

  It is repeatable, and a nested build file's directives **append** to the set
  they inherit rather than replacing it, so the effective set does not depend on
  which directory asks. The value is a basename and not a path or a glob:
  a basename is all the traversal ever compares against, so anything else is
  refused out loud rather than silently excluding nothing — and excluding one
  named path is what `# gazelle:ts_exclude ./web/coverage` already does.

- **`# gazelle:ts_npm_mapping <file>` points named npm packages at labels of
  your own.** The value is a workspace-root-relative JSON file of npm package
  name → Bazel label, for a package that has to come from somewhere other than
  the hub — vendored, patched, or built by a target in this repo. It **overlays**
  the pnpm lockfile inventory rather than replacing it: a name the file gives a
  label keeps that label, and every name it leaves out keeps the lockfile's, so
  a file listing three overrides does not shrink the workspace's inventory to
  three packages. Repeatable, and inherited, so a subtree can overlay again on
  top of what an ancestor mapped.

  This is not `# gazelle:ts_npm_hub`, which names the *repo* a whole tree's bare
  specifiers resolve into: use the hub directive when the packages are the same
  and the repo differs, and this one when a single package's label is not the
  hub's at all.
