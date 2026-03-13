/**
 * About route — renders the about page (/about).
 *
 * Remix file-based routing: this file maps to the "/about" route.
 */

import type { MetaFunction } from "@remix-run/react";
import type React from "react";

export const meta: MetaFunction = () => {
  return [{ title: "About | Remix + rules_typescript" }];
};

export default function About(): React.ReactElement {
  return (
    <div>
      <h1>About</h1>
      <p>
        This example demonstrates Remix SPA mode with Bazel and rules_typescript.
        Routes are staged into a writable directory before bundling, allowing the
        Remix Vite plugin to discover and process them.
      </p>
    </div>
  );
}
