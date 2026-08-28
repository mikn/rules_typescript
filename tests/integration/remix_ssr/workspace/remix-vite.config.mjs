import { vitePlugin as remix } from "@remix-run/dev";

// A plain Remix config: remix_build stages it beside the node_modules tree and
// builds inside a root it owns, so nothing here is Bazel-specific. Neither key
// would survive ts_bundle's generated config, which reads only `plugins`.
export default {
  plugins: [remix({ manifest: true })],
  build: { manifest: true },
};
