// wrangler's declaration entry point opens with
//   import { ... } from '@cloudflare/workers-types';
// and that package is a global script -- 15k lines, no top-level import or
// export -- so its `interface Element` merges into the one lib.dom declared,
// and `append` takes no Node any more. An `import()` is a module load like any
// other; nothing here mentions Cloudflare.
export function attach(host: Element, child: HTMLDivElement): void {
  host.append(child);
}

export function later(): void {
  void import("wrangler");
}
