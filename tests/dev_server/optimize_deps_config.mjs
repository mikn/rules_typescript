/**
 * A vite_config whose plugin asks for a dependency to be pre-bundled, which is
 * how a framework plugin does it -- TanStack Start's `config` hook lists react
 * and react-dom this way.
 *
 * `optimizeDeps.include` is resolved by Vite itself with no importer at all --
 * `resolve(environment, id, undefined)`, which walks up from `root`. No plugin
 * runs on that path, so nothing in the generated config can answer for it: the
 * only thing that makes it resolve is a real node_modules on that walk.
 */
export default {
  plugins: [
    {
      name: 'devserver-optimize-deps',
      config() {
        return { optimizeDeps: { include: ['zod'] } };
      },
    },
  ],
};
