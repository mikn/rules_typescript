/**
 * Tailwind v4 has no config file of its own: it is a Vite plugin, and this is
 * the vite_config that installs it. The plugin drives @tailwindcss/oxide, a
 * platform-specific native module, so this is also what proves the npm tree
 * placed the right one of the thirteen the lockfile carries.
 */

import tailwindcss from '@tailwindcss/vite';
import type { UserConfig } from 'vite';

const config: UserConfig = { plugins: [tailwindcss()] };

export default config;
