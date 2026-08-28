import { headers } from "next/headers";

// force-dynamic keeps this route out of prerender-manifest.json: it is rendered
// per request, so the Host it echoes is the one the request carried.
export const dynamic = "force-dynamic";

export default async function Dynamic() {
  const host = (await headers()).get("host");

  return (
    <main>
      <h1>Server-rendered on demand</h1>
      <p>Host: {host}</p>
      <nav>
        <a href="/">Home</a>
      </nav>
    </main>
  );
}
