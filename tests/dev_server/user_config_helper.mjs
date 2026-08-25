/**
 * The sibling module tests/dev_server/user_config_relative.mjs imports.
 *
 * It carries the plugin rather than just existing, so the two halves of the
 * vite_config boundary are asserted the same way: :vite_config_boundary_test
 * shows this file in the source tree while the config that imports it fails to
 * load without vite_config_srcs, and
 * :dev_with_composed_user_config_behaviour_test shows the very same plugin
 * installed and transforming once vite_config_srcs stages it beside the copy.
 */

export function userPlugin() {
  return {
    name: 'devserver-user-config',
    transform(code, id) {
      if (!id.endsWith('/app.ts')) return null;
      return `${code}\nexport const userConfigMarker = "USER_CONFIG_PLUGIN_RAN";\n`;
    },
  };
}
