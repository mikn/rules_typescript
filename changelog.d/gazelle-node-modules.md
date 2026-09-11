### Added

- **Gazelle writes every importer's `node_modules`.** In each directory the
  lockfile's `importers:` names it writes `node_modules(name = "node_modules",
  deps = [<hub label per declared name>], parent = "//<importer above>:node_modules")`,
  one `node_modules_member(name = "node_modules/<member>", member = "@npm//:<member>")`
  per `link:` entry, and in the lockfile's package `npm_virtual_store(name =
  "node_modules/.pnpm")`; a directory that is no importer withdraws them. A
  member a target imports by name is spelled as the nearest importer's link
  target, `//web:node_modules/@acme/ui`, where it was the hub's view; a member
  no importer at or above the package links gets no label and one line naming
  it, since a target resolves through its importers alone.
