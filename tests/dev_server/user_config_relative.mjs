/**
 * The far side of the vite_config boundary: only this one file is copied into
 * bazel-bin, so the sibling it imports is not there to be found even though it
 * sits right next to it in the source tree.
 */

import { userPlugin } from './user_config_helper.mjs';

export default { plugins: [userPlugin()] };
