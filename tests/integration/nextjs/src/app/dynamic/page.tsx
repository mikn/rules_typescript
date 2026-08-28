import { headers } from "next/headers";

export const dynamic = "force-dynamic";

// The nonce is what no static export can imitate: two requests to this route
// have to answer with different HTML.
export default async function Dynamic() {
  const host = (await headers()).get("host");
  return (
    <p>
      DYNAMIC_MARKER host={host} nonce={crypto.randomUUID()}
    </p>
  );
}
