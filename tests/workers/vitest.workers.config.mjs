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
 * preserveSymlinks must be off, and this is the line that is easiest to lose:
 * ts_test's own layer turns it on, the pool resolves modules for workerd through
 * a second path, and a lexical path there is a second module identity for the
 * same file. Without it the run dies as
 * `TypeError: Cannot read properties of undefined (reading 'config')` from
 * inside the pool runner -- which reads like the plugin API being wrong rather
 * than a resolution setting.
 *
 * The paths are absolute because a relative one resolves against the vite root,
 * which is the runfiles root rather than this package. TS_TEST_PACKAGE_DIR is
 * where ts_test put the compiled worker.
 */

import { join } from 'node:path';

import { cloudflareTest } from '@cloudflare/vitest-pool-workers';

const pkg = process.env.TS_TEST_PACKAGE_DIR;

export default {
  resolve: { preserveSymlinks: false },
  plugins: [
    cloudflareTest({
      remoteBindings: false,
      wrangler: { configPath: join(pkg, 'wrangler.jsonc') },
    }),
  ],
};
