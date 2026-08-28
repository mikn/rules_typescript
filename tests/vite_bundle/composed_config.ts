/**
 * A vite_config in the shape a real framework config has: TypeScript, with the
 * plugin coming from a relatively-imported local module rather than from this
 * file.
 *
 * Both properties are the assertion. A .ts config cannot be reached by a plain
 * dynamic import, so it loads only because the generated config goes through
 * Vite's own loader; and the sibling resolves only because vite_config_srcs
 * staged it beside this file under bazel-bin.
 */

import type { UserConfig } from 'vite';

import { composedPlugin } from './composed_plugin';

const config: UserConfig = { plugins: [composedPlugin()] };

export default config;
