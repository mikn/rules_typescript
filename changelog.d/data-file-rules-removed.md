### Breaking — rules

- **`css_library`, `css_module`, `asset_library` and `json_library` are gone,
  with `CssInfo`, `CssModuleInfo` and `AssetInfo`.** A `.css`, an image, a
  `.json` or any other file a module imports is a src of the `ts_compile` that
  imports it: staged beside the compiled `.js`, carried to `ts_test`,
  `ts_binary` and `ts_dev_server` in `JsInfo.transitive_data_files`, and typed
  by the tsconfig -- `vite/client` in `types`, a `declare module "*.svg"` in
  `srcs`, a `.json` from its contents under `resolveJsonModule`. A dep's `.json`
  is a tsgo input of the consumer too, so a relative import of one across
  packages resolves. A workspace member's data srcs sit in its `@npm//:<name>`
  link at their package-relative paths, so its `.js` reaches them beside itself
  when imported by name. A `*.module.css` is Vite's own CSS modules in the bundle
  and the dev server, and under vitest the class-name proxy its
  `css.modules.classNameStrategy` shapes; the postcss-modules compiler, its Vite
  plugin and the `allowArbitraryExtensions` baseline key that served its `.d.ts`
  are gone.
  Gazelle writes none of the four kinds and knows no
  `# gazelle:ts_asset_declaration_type`. The edit: move each such file into the
  importing `ts_compile`'s `srcs`, delete the rule, and put the type the import
  gets in the tsconfig or a declaration file.
