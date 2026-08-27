import type { ReactElement } from "react";
import { useLoaderData } from "@remix-run/react";
import { panelLabel } from "./helper";

export function loader(): { label: string } {
  return { label: panelLabel() };
}

export default function Panel(): ReactElement {
  const data = useLoaderData<typeof loader>();
  return <h1>{data.label}</h1>;
}
