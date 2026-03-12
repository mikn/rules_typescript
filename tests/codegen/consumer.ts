// Consumer TypeScript source file that imports the generated output.
// Verifies that ts_compile can use ts_codegen output as srcs.

import { GENERATED } from "./generated.js";

export function isGenerated(): boolean {
    return GENERATED;
}
