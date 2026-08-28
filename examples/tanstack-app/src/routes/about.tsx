import { createFileRoute } from "@tanstack/react-router";

function AboutComponent() {
  return (
    <div className="page page--about">
      <h1>tanstack-about-route-marker</h1>
      <p>
        Routes are compiled and type-checked by ts_compile, and bundled from the
        staged sources by the TanStack Start Vite plugin.
      </p>
    </div>
  );
}

export const Route = createFileRoute("/about")({
  component: AboutComponent,
});
