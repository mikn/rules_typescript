### Added

- `ts_compile` and `ts_test` accept `emit = False` for TypeScript source inputs consumed by Vitest or the OJ dev server. Type checking remains a validation action. Workspace members retain their source manifests and importer-specific npm dependencies; runtimes requiring emitted JavaScript reject source-only dependency closures.
