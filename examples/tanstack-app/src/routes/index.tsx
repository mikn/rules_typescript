/**
 * Index route component — renders the home page (/).
 */

import type { ReactElement } from "react";

export function IndexComponent(): ReactElement {
  return (
    <div className="page page--home">
      <h1>Welcome to the TanStack Router example</h1>
      <p>
        This is a <strong>rules_typescript</strong> example showing how to
        compile a TanStack Router app with Bazel.
      </p>
    </div>
  );
}
