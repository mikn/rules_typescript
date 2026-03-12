/**
 * Application entry point — exported for ts_binary to bundle.
 *
 * This is the package boundary for //src/app. ts_binary uses this as
 * its entry_point, bundling the entire dependency graph into a single
 * ESM output file.
 */
export { App } from "./App";
