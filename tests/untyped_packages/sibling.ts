// A dependent of :shimmed that still resolves both packages: the attribute is
// this target's own, and `Fetcher` is declared by nothing but
// @cloudflare/workers-types' global script, so naming it compiles only while
// that script is in this program.
import { attach } from "./shimmed";

export function reuse(host: Element, child: HTMLDivElement): void {
  attach(host, child);
}

export function target(fetcher: Fetcher): Fetcher {
  return fetcher;
}

export function later(): void {
  void import("wrangler");
}
