### Fixed

- **`wrangler_types` removes `build` from the config before `wrangler types`
  reads it.** The generator staged the config verbatim, and `wrangler types`
  runs `build.command` before it resolves `main`, logging a failure and going on
  without the entry. The action has no `PATH` and stages no `package.json`, so a
  worker whose command is `pnpm run build` got a `worker-configuration.d.ts`
  without `Cloudflare.GlobalProps.mainModule` although `main` was staged for it.
  The staged copy now has `build` removed at the top level and under every
  `env`, through wrangler's `experimental_patchConfig`, so nothing the config
  names runs in the action and the output is the one the same config with no
  `build` gives. wrangler refuses to patch a `.toml` containing `#`, and the
  generator fails naming the config.
