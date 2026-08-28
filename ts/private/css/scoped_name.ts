import { createHash } from "node:crypto";

// The scoped class name css_module puts in the exports map, and the one the
// bundler must reproduce. A pure function of (local name, stylesheet bytes):
// no filename, no cwd, no line number, so a build in a different sandbox or
// output base mints the same name.
export function scopedName(localName: string, cssText: string, hashPrefix = ""): string {
  return `_${localName}_${contentHash(cssText, hashPrefix)}`;
}

function contentHash(cssText: string, hashPrefix = ""): string {
  return createHash("sha256")
    .update(hashPrefix + cssText, "utf8")
    .digest("hex")
    .slice(0, 8);
}
