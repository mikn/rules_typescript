// The test file's `import()` again, so this compile is an assertion only while
// the exclusion reaches the setup compile too.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export function later(): void {
  void import("wrangler");
}
