import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";

// The plugin splits this handler into its own server-only module and keys it in
// the server bundle's resolver by sha256("<root-relative path>--<name>").
const readGreeting = createServerFn().handler(
  () => "greeting-served-by-the-server-bundle",
);

function IndexComponent() {
  const greeting = Route.useLoaderData();
  return (
    <div className="page page--home">
      <h1>tanstack-index-route-marker</h1>
      <p>{greeting}</p>
    </div>
  );
}

export const Route = createFileRoute("/")({
  loader: () => readGreeting(),
  component: IndexComponent,
});
