import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";

const readGreeting = createServerFn().handler(() => "acme-server-fn-marker");

function IndexComponent() {
  return (
    <div>
      <h1>acme-index-route-marker</h1>
      <p>{Route.useLoaderData()}</p>
    </div>
  );
}

export const Route = createFileRoute("/")({
  loader: () => readGreeting(),
  component: IndexComponent,
});
