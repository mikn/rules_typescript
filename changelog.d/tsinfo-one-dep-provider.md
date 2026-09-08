### Breaking — providers

- **`TsInfo` is the one provider a dep returns; `JsInfo` and
  `TsDeclarationInfo` are gone.** Its fields: `js`, `js_maps`, `declarations`,
  `data` and `sources` for what the target itself produces, `transitive_js`,
  `transitive_js_maps`, `transitive_declarations` and `transitive_data` for the
  closure over its first-party deps, and `npm_packages` for the packages a
  consumer links in its forest. `ts_compile`, `ts_codegen` and `ts_binary`
  return it over their outputs; an npm package target returns one naming its
  closure and nothing by path, since its files reach a consumer through the
  node_modules tree, so an npm dep's `.js` no longer sits in a consumer's
  runfiles beside the tree that holds it; a workspace member's hub view forwards
  the member's. `deps` on `ts_compile` and `ts_test` and `entry_point` on
  `ts_binary` and `ts_dev_server` take any target providing it.
  `//ts:defs.bzl` exports `TsInfo` and, new, `DevServerInfo`; it no longer
  exports `refresh_workspace_files`, the copier `ts_refresh_tsconfig`
  instantiates, and `ts_dev_server` loses `bundler`, whose one job was a
  `BUNDLER_BINARY` variable for a server implementation that never existed --
  `server` and `DevServerInfo` are how another server plugs in. The edit:
  `JsInfo.js_files`, `.js_map_files`, `.data_files`, `.source_files`,
  `.transitive_js_files`, `.transitive_js_map_files` and
  `.transitive_data_files` become `TsInfo.js`, `.js_maps`, `.data`, `.sources`,
  `.transitive_js`, `.transitive_js_maps` and `.transitive_data`;
  `TsDeclarationInfo.declaration_files`, `.transitive_declaration_files` and
  `.transitive_npm_packages` become `TsInfo.declarations`,
  `.transitive_declarations` and `.npm_packages`.
