/**
 * mock_plugin.mjs — Minimal Vite plugin for testing vite_config injection.
 *
 * This plugin uses the renderChunk hook to prepend a sentinel variable
 * declaration to every output chunk. The verification test checks for this
 * variable to confirm that the user-supplied vite_config was loaded and its
 * plugins were applied.
 *
 * Note: Vite strips pure comments (/* ... *\/) from lib mode output, so we
 * inject an actual JavaScript variable declaration as the sentinel instead.
 */

/** @type {import('vite').Plugin} */
const mockPlugin = {
  name: "rules-typescript-mock-plugin",
  renderChunk(code) {
    return "const _VITE_PLUGIN_INJECTED = true;\n" + code;
  },
};

export default {
  plugins: [mockPlugin],
};
