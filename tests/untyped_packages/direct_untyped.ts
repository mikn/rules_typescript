// The same direct dep, excluded: `files` loses the entry point too, so the
// globals it carries are gone -- naming `Fetcher` here would be TS2304.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}
