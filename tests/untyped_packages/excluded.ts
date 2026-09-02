// leaks.ts, with @cloudflare/workers-types kept out of this target's program.
// Nothing here changes: the dep is the same, the import is the same, and the
// DOM call is the same one that failed.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export function later(): void {
  void import("wrangler");
}
