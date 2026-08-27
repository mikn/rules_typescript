import type { ReactElement } from "react";
import { useActionData, useLoaderData } from "@remix-run/react";

export function loader(): { title: string } {
  return { title: "acme-dash-settings-loader" };
}

export async function action({
  request,
}: {
  request: Request;
}): Promise<{ echoed: string }> {
  const form = await request.formData();
  return { echoed: String(form.get("field") ?? "") };
}

export default function Settings(): ReactElement {
  const data = useLoaderData<typeof loader>();
  const posted = useActionData<typeof action>();
  return (
    <form method="post">
      <h2>{data.title}</h2>
      <p>{posted?.echoed ?? "acme-no-action-yet"}</p>
      <input name="field" />
    </form>
  );
}
