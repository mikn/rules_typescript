// The other half of the exclusion: wrangler itself is out of the program, so
// `import("wrangler")` has no `paths` key to resolve through and wrangler.d.ts
// is what answers it. The global script one hop behind it never loads.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export async function later(): Promise<void> {
  const { unstable_startWorker } = await import("wrangler");
  unstable_startWorker();
}
