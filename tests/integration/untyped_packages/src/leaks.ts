export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export function later(): void {
  void import("wrangler");
}
