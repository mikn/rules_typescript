import type { ReactElement, ReactNode } from "react";
import { Links, Meta, Outlet, Scripts } from "@remix-run/react";

export function Layout({ children }: { children: ReactNode }): ReactElement {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  );
}

export default function App(): ReactElement {
  return <Outlet />;
}
