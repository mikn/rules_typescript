// Consumes the workspace alias generated from an importer-relative link:
// entry (tests/npm/nested → link:../../../packages/nested-shared).
import { shout } from "nested-shared";

export const loud: string = shout("World");
