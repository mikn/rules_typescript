// TraceItem is exported by the package's index.ts and declared as a global by
// its index.d.ts; only the module answers an import.
import type { TraceItem } from "@cloudflare/workers-types";

export const name = (t: TraceItem) => t.scriptName;
