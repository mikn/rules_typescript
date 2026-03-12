/**
 * About route component — renders the about page (/about).
 */

import type { ReactElement } from "react";

export function AboutComponent(): ReactElement {
  return (
    <div className="page page--about">
      <h1>About</h1>
      <p>
        This example demonstrates TanStack Router with Bazel and
        rules_typescript. Routes are compiled and type-checked using oxc and
        tsgo.
      </p>
    </div>
  );
}
