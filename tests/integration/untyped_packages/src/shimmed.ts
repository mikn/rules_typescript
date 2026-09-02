export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export async function later(): Promise<void> {
  const { unstable_startWorker } = await import("wrangler");
  unstable_startWorker();
}
