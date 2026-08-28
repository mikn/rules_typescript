import type { ReactElement } from "react";
import { Outlet, useLoaderData } from "@remix-run/react";

export function loader(): { section: string } {
  return { section: "acme-dash-layout-loader" };
}

export default function DashLayout(): ReactElement {
  const data = useLoaderData<typeof loader>();
  return (
    <section>
      <h1>{data.section}</h1>
      <Outlet />
    </section>
  );
}
