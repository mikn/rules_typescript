export function loader(): Response {
  return new Response("acme-resource-route-body", {
    headers: { "Content-Type": "text/plain" },
  });
}
