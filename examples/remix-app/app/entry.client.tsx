/**
 * Remix client entry point.
 *
 * In Remix SPA mode (ssr: false), this file is the browser entry that
 * hydrates the React application.
 *
 * ── Bazel integration note ───────────────────────────────────────────────────
 *
 * This file is compiled by Bazel (ts_compile :entry_client) to produce
 * bazel-bin/examples/remix-app/app/entry.client.js.
 *
 * It is ALSO staged by staging_srcs into _staging/app/entry.client.tsx so
 * the Remix vitePlugin can discover it as the client entry.
 *
 * resolve.alias in the generated vite.config.mjs maps
 * "app/entry.client" → the Bazel-compiled .js. However, Remix's virtual
 * module system may resolve the entry differently — see BUILD.bazel for
 * the honest limitations.
 */

import { RemixBrowser } from "@remix-run/react";
import { startTransition, StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";

startTransition(() => {
  hydrateRoot(
    document,
    <StrictMode>
      <RemixBrowser />
    </StrictMode>,
  );
});
