export const routes: readonly string[] = ["/health"];

export default {
  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    if (routes.includes(url.pathname)) {
      return new Response("ok", { status: 200 });
    }
    return new Response("not found", { status: 404 });
  },
};
