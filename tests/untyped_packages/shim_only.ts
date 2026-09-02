// The shim without the exclusion, which is the composition that does NOT work:
// TypeScript resolves the specifier through `paths` and adds wrangler.d.ts to
// the program before the checker ever asks about an ambient module, so the
// global script one hop behind it arrives anyway and `Element.append` is
// widened exactly as in leaks.ts.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export async function later(): Promise<void> {
  const { unstable_startWorker } = await import("wrangler");
  unstable_startWorker();
}
