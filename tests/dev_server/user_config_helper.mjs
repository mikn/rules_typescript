/**
 * The sibling module tests/dev_server/user_config_relative.mjs imports. It is a
 * runfile of :vite_config_boundary_test so the test can show it exists in the
 * source tree, which is what makes the failure about hermeticity rather than
 * about a missing file.
 */

export function userPlugin() {
  return { name: 'devserver-user-config-relative' };
}
