import type { ReactNode } from "react";
import { createRootRoute, Outlet, Scripts } from "@tanstack/react-router";
import { Banner } from "../components";

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
      </head>
      <body>
        <div id="root">{children}</div>
        <Scripts />
      </body>
    </html>
  );
}

export const Route = createRootRoute({
  shellComponent: RootDocument,
  component: () => (
    <Banner>
      <Outlet />
    </Banner>
  ),
});
