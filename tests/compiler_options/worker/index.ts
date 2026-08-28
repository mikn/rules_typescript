const handler: ExportedHandler<Env> = {
  async fetch(request, env) {
    const cached = await caches.default.match(request);
    if (cached) {
      return cached;
    }
    const session = await env.SESSIONS.get(request.url);
    return new Response(session, { status: session ? 200 : 404 });
  },
};

export default handler;
