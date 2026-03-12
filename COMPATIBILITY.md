# Compatibility

## Bazel Versions

| Bazel Version | Support Level |
|---------------|--------------|
| 9.x | Fully supported (primary development target) |
| 8.x | Should work (bzlmod required) |
| 7.x | Untested (bzlmod available but may have differences) |
| < 7.0 | Not supported (no bzlmod) |

rules_typescript requires bzlmod (MODULE.bazel). WORKSPACE-based setups are not supported.

## Platforms

| Platform | Compilation | Testing | Bundling | Dev Server |
|----------|------------|---------|----------|------------|
| Linux x86_64 | Full | Full | Full | Full |
| Linux ARM64 | Full | Full | Full | Full |
| macOS x86_64 | Full | Full | Full | Full |
| macOS ARM64 | Full | Full | Full | Full |
| Windows x86_64 | Partial (node_modules only) | Not yet | Not yet | Not yet |

## Versioning Policy

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

**Pre-1.0 (current):** Minor versions may contain breaking changes. Patch versions are backwards-compatible.

**Post-1.0 (future):** Major versions for breaking changes, minor for features, patch for fixes.

## Public API Surface

### Stable (will not break without major version bump post-1.0)
- `ts_compile`, `ts_test`, `ts_binary`, `ts_bundle` rules and their documented attributes
- `JsInfo`, `TsDeclarationInfo`, `BundlerInfo`, `CssInfo`, `AssetInfo` providers
- `npm_translate_lock` module extension
- Gazelle `ts_compile`, `ts_test` generation
- All `# gazelle:ts_*` directives

### Experimental (may change in minor versions)
- `ts_dev_server` rule and attributes
- `ts_codegen` rule
- `ts_lint` rule
- `ts_npm_publish` rule
- `vite_bundler` rule
- Gazelle codegen auto-detection
- Vite plugin (`vite/src/`)

## Deprecation Policy

Deprecated features will be:
1. Marked with a warning message for at least one minor version
2. Documented in CHANGELOG.md
3. Removed in the next major version (or next minor version pre-1.0)
