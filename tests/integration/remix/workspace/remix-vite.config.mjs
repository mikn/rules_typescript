import { vitePlugin as remix } from "@remix-run/dev";

// staging_srcs copies the app/ sources into a writable directory and points
// VITE_STAGING_ROOT at it; Remix scans routes and writes its codegen there.
// buildDirectory redirects Remix's own output into the Bazel-declared dir,
// which it would otherwise bypass by writing to <root>/build/.
export default {
  root: process.env["VITE_STAGING_ROOT"],
  plugins: [
    remix({
      ssr: false,
      buildDirectory: process.env["VITE_OUT_DIR"],
    }),
  ],
};
