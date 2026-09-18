### Breaking — gazelle

- **Gazelle no longer writes `ts_dev_server`.** A package holding a `main.ts`,
  `main.tsx`, `app.ts`, `app.tsx` or an `index.html` used to get a
  `ts_dev_server` named `dev` on the first run that saw it, with `entry_point`,
  `port = 5173`, `plugin` and `visibility` filled in and `node_modules` left
  empty. Gazelle knows no such kind now: it writes no `dev` target and neither
  merges nor removes one already in a BUILD file. The edit: none for a rule you
  have, which comes through every run as written, load symbol included, `# keep`
  or not; a new application writes its own
  `ts_dev_server(name = "dev", entry_point = ":app", node_modules = ":node_modules", port = 5173)`
  beside the `ts_compile` it serves. See the Dev Server guide.
