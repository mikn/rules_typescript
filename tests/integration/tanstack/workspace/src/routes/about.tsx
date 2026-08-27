import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

const AboutSearch = z.object({ page: z.number().int().positive().default(1) });

function AboutComponent() {
  const { page } = Route.useSearch();
  return (
    <div>
      <h1>acme-about-route-marker</h1>
      <p>page {page}</p>
    </div>
  );
}

export const Route = createFileRoute("/about")({
  validateSearch: (input: Record<string, unknown>) => AboutSearch.parse(input),
  component: AboutComponent,
});
