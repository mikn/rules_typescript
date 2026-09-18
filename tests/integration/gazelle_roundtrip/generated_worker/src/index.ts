export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const cached = await env.CACHE.get(new URL(request.url).pathname);
    return new Response(cached ?? env.GREETING);
  },
} satisfies ExportedHandler<Env>;
