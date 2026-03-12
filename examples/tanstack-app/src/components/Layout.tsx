/**
 * Layout component — the application shell.
 *
 * In TanStack Router, the root route's component wraps all child routes.
 * This Layout provides navigation and a content area.
 *
 * All exports carry explicit type annotations for isolated declarations.
 */

import type { ReactElement, ReactNode } from "react";
import { Link } from "@tanstack/react-router";

export interface LayoutProps {
  children?: ReactNode;
}

export function Layout(props: LayoutProps): ReactElement {
  return (
    <div className="app-layout">
      <nav className="app-nav">
        <Link to="/">Home</Link>
        <Link to="/about">About</Link>
        <Link to="/users">Users</Link>
      </nav>
      <main className="app-content">{props.children}</main>
    </div>
  );
}
