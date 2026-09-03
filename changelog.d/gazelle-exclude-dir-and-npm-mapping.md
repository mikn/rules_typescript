### Added

- **`# gazelle:ts_exclude_dir <basename>` keeps Gazelle out of a directory
  entirely.** `# gazelle:ts_ignore` suppresses generation for the directory
  whose build file carries it; `coverage/`, `storybook-static/` and the rest
  have no build file to put a directive in. This directive is declared in an
  ancestor and names a basename, so nothing is generated in any directory of
  that name below it, on top of the built-in `.next`, `.nuxt`, `.svelte-kit`,
  `dist`, `build` and `node_modules`.

  It is repeatable, and a nested build file's directives append to the set they
  inherit, so the effective set does not depend on which directory asks. The
  whole value is one basename; a path, a glob, or a list of names is refused.
  The traversal compares one directory basename literally. `ts_exclude` does
  not reach a directory either: its patterns drop files from a target's `srcs`,
  and under the default `every-dir` boundary that directory is its own target.

- **`# gazelle:ts_npm_mapping <file>` points named npm packages at labels of
  your own.** The value is a workspace-root-relative JSON file of npm package
  name → Bazel label, for a package that comes from somewhere other than the
  hub: vendored, patched, or built by a target in this repo. It overlays the
  pnpm lockfile inventory. A name the file gives a label keeps that label, and
  every name it leaves out keeps the lockfile's, so a file listing three
  overrides does not shrink the workspace's inventory to three packages.
  Repeatable, and inherited, so a subtree can overlay again on top of what an
  ancestor mapped.

  `# gazelle:ts_npm_hub` names the repo a whole tree's bare specifiers resolve
  into. Use the hub directive when the packages are the same and the repo
  differs, and this one when a single package's label is not the hub's.
