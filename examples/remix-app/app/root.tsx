/**
 * Root layout component for the Remix app.
 *
 * In Remix, root.tsx wraps all routes. It provides the HTML shell including
 * the <head>, <body>, and the <Outlet> that renders matched child routes.
 */

import type { ReactElement, ReactNode } from "react";
import { Links, Meta, Outlet, Scripts, ScrollRestoration } from "@remix-run/react";

export function Layout({ children }: { children: ReactNode }): ReactElement {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export default function App(): ReactElement {
  return <Outlet />;
}
