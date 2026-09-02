// A target that resolves @cloudflare/workers-types and excludes nothing, so an
// editor config covering it and :excluded together has two answers to give for
// one `paths` key. `Fetcher` comes from the global script and from nowhere else.
export function target(fetcher: Fetcher): Fetcher {
  return fetcher;
}

export function later(): void {
  void import("wrangler");
}
