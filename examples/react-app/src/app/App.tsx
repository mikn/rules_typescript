/**
 * Root application component.
 *
 * Imports from //src/components (React components) and //src/validation
 * (Zod schema), demonstrating cross-package dependencies with .d.ts boundaries.
 *
 * Changing components/validation without changing their public API does NOT
 * cause App.tsx to be recompiled — that is the .d.ts compilation boundary.
 */

import type { ReactElement } from "react";
import { Counter } from "../components";
import { validateCreateUser } from "../validation";
import type { CreateUserInput } from "../validation";

// Validate a user at module init time (demonstrating runtime Zod usage).
const _demoUser: CreateUserInput = validateCreateUser({
  name: "App Demo User",
  email: "demo@example.com",
});

export function App(): ReactElement {
  return (
    <div className="app">
      <header>
        <h1>React App — rules_typescript example</h1>
        <p>User: {_demoUser.name}</p>
      </header>
      <main>
        <Counter label="Primary Counter" initialValue={0} step={1} />
        <Counter label="Step-5 Counter" initialValue={10} step={5} />
      </main>
    </div>
  );
}
