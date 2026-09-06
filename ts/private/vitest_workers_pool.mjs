// Layer 1's half for @cloudflare/vitest-pool-workers, imported by the generated
// config whenever a config file is set; a no-op unless that config installs the pool.
import { existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

const POOL_PLUGIN = '@cloudflare/vitest-pool-workers';

export const hasWorkersPool = (plugins) =>
  (Array.isArray(plugins) ? plugins.flat(Infinity) : []).some((p) => p && p.name === POOL_PLUGIN);

const cleanUrl = (id) => id.replace(/[?#].*$/, '');
const isRelative = (id) => /^\.\.?(\/|$)/.test(id);
const isBare = (id) => !/^[./\\]/.test(id);
const isVirtual = (id) => id.startsWith('\0') || /^[a-z][a-z0-9+.-]*:/i.test(id);
const isBazelOutput = (path) => /\/bazel-out\/[^/]+\/bin\//.test(path);

// bazel-out/<cfg>/bin/<short_path> is staged at <workspace>/<short_path> (an
// external repo's under the runfiles root); a runfiles path is its own.
export const runfilesPath = (path, workspaceDir) => {
  const runfilesRoot = dirname(workspaceDir);
  if (path.startsWith(runfilesRoot + '/')) return path;
  const m = /^.*?\/bazel-out\/[^/]+\/bin\/(.*)$/.exec(path);
  if (!m) return null;
  const external = /^external\/([^/]+)\/(.*)$/.exec(m[1]);
  return external ? resolve(runfilesRoot, external[1], external[2]) : resolve(workspaceDir, m[1]);
};

// On realpaths a compiled module sits in bazel-out, beside every output the
// output base holds and with no node_modules above it.
export const runfilesImports = (root, workspaceDir) => ({
  name: 'rules_typescript:runfiles-imports',
  enforce: 'pre',
  async resolveId(id, importer, opts) {
    if (isVirtual(id)) return null;
    const nested = { ...opts, skipSelf: true };
    const from = importer && !importer.startsWith('\0') && !importer.includes('/node_modules/')
      ? runfilesPath(cleanUrl(importer), workspaceDir)
      : null;
    let resolved = null;
    if (from && isRelative(id)) resolved = await this.resolve(id, from, nested);
    else if (from && isBare(id)) resolved = await this.resolve(id, resolve(root, '__rules_typescript_anchor__.js'), nested);
    if (!resolved) resolved = await this.resolve(id, importer, nested);
    if (!resolved || resolved.external) return resolved;
    const real = cleanUrl(resolved.id);
    if (!isBazelOutput(real) || real.includes('/node_modules/') || real.startsWith(dirname(workspaceDir) + '/')) return resolved;
    const staged = runfilesPath(real, workspaceDir);
    if (!staged || !existsSync(staged)) {
      this.error(
        `rules_typescript: "${id}" resolved to ${real}, a build output this test's runfiles do not hold; ` +
          'a dep or data entry has to stage it (an asset_library dep of the ts_compile for a wrangler rules module).',
      );
    }
    return resolved;
  },
});

// A user value still wins over the layer's, as any scalar of the layer above.
export const workersPoolLayer = (bazelLayer, user, workspaceDir) => {
  if (!hasWorkersPool(user.plugins)) return bazelLayer;
  return {
    ...bazelLayer,
    resolve: { ...bazelLayer.resolve, preserveSymlinks: false },
    plugins: [...bazelLayer.plugins, runfilesImports(bazelLayer.root, workspaceDir)],
  };
};
