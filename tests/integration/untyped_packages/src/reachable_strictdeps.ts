import type { Fetcher } from "@cloudflare/workers-types";

export const bind = (f: Fetcher): Fetcher => f;
