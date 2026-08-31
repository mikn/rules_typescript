// A ".ts" specifier is TS5097 unless allowImportingTsExtensions is on, and the
// only layer that turns it on here is the named tsconfig.
import { helper } from "./ts_ext_helper.ts";

export const value: number = helper();
