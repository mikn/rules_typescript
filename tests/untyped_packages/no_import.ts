// The control: the same DOM call as leaks.ts, and the same wrangler dep, with
// nothing that loads a module. `Element.append` is lib.dom's here.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}
