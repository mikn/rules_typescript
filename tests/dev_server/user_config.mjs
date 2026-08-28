/**
 * A user-supplied vite_config, as the vite_config attr documents one: a bare
 * npm import for the plugin package, and a default export with a plugins array.
 *
 * The bare `zod` import is the assertion. It resolves only because the rule
 * loads a COPY of this file out of bazel-bin, beside the npm tree the
 * node_modules attr built; loaded from the source tree it would reach for a
 * source-tree node_modules that this ruleset does not have.
 */

import { z } from 'zod';

const marker = z.string().parse('USER_CONFIG_PLUGIN_RAN');

export default {
  plugins: [
    {
      name: 'devserver-user-config',
      transform(code, id) {
        if (!id.endsWith('/app.ts')) return null;
        return `${code}\nexport const userConfigMarker = "${marker}";\n`;
      },
    },
  ],
};
