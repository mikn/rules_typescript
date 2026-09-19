/**
 * The Workers pool, installed the one way that works.
 *
 * @cloudflare/vitest-pool-workers exports two things and only one of them is
 * enough. `cloudflarePool()` is just a PoolRunnerInitializer: it boots workerd
 * and nothing else. `cloudflareTest()` is a Vite plugin that installs that pool
 * itself AND owns `cloudflare:test` -- its resolveId maps the specifier to a
 * virtual id and its load returns the runtime's bytes with `import "<main>"`
 * appended. The pool deliberately forwards `cloudflare:test` to Vite rather than
 * externalising it to workerd like every other `cloudflare:*` specifier, so with
 * no plugin registered there is nothing to resolve it and vitest falls back to
 * node package resolution, which fails. Hence `plugins`, not `test.pool`.
 *
 * `wrangler.configPath` is relative because ts_test roots vite at the package,
 * which is also where the compiled worker the config's `main` names is staged.
 */

import { cloudflareTest } from '@cloudflare/vitest-pool-workers';

export default {
  plugins: [
    cloudflareTest({
      remoteBindings: false,
      wrangler: { configPath: 'wrangler.jsonc' },
    }),
  ],
};
