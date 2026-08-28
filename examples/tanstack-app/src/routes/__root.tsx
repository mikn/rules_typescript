import type { ReactNode } from "react";
import {
  createRootRoute,
  HeadContent,
  Outlet,
  Scripts,
} from "@tanstack/react-router";
import { Layout } from "../components";

// The shell renders the whole document: TanStack Start streams this, so
// index.html never reaches the browser even though ts_bundle requires one.
function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <HeadContent />
      </head>
      <body>
        <div id="root">{children}</div>
        <Scripts />
      </body>
    </html>
  );
}

function RootComponent() {
  return (
    <Layout>
      <Outlet />
    </Layout>
  );
}

export const Route = createRootRoute({
  head: () => ({ meta: [{ title: "TanStack Start on Bazel" }] }),
  shellComponent: RootDocument,
  component: RootComponent,
});
