/**
 * Index route — renders the home page (/).
 *
 * Remix file-based routing: this file maps to the "/" route.
 * The filename _index.tsx follows Remix v2 flat-file convention.
 */

import type { MetaFunction } from "@remix-run/react";
import type React from "react";

export const meta: MetaFunction = () => {
  return [
    { title: "Remix + rules_typescript" },
    { name: "description", content: "A minimal Remix SPA example with Bazel." },
  ];
};

export default function Index(): React.ReactElement {
  return (
    <div>
      <h1>Welcome to Remix (rules_typescript)</h1>
      <p>
        This is a minimal Remix SPA app built with Bazel and rules_typescript.
        Route files are staged into a writable directory so the Remix Vite plugin
        can scan them and generate the route manifest.
      </p>
    </div>
  );
}
