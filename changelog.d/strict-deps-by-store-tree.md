### Fixed

- **An npm alias passes the strict-deps check.** The check named a store
  file's package by the name segment of its realpath -- `tailwindcss` for the
  file `tailwindcss-v3` links, a name no importer declares -- and failed the
  action with `under a package the npm closure does not hold`. The ownership
  manifest's `npm-direct` and `npm` rows now carry the store key each link
  enters, and the check keys a file on the tree it sits in,
  `node_modules/.pnpm/<key>/node_modules/<name>/`.
