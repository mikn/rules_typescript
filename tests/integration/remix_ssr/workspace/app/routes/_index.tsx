import type { ReactElement } from "react";
import { useLoaderData } from "@remix-run/react";

export function loader(): { greeting: string } {
  return { greeting: "acme-index-loader-value" };
}

export default function Index(): ReactElement {
  const data = useLoaderData<typeof loader>();
  return (
    <main>
      <h1>acme-index-route-marker</h1>
      <p>{data.greeting}</p>
    </main>
  );
}
