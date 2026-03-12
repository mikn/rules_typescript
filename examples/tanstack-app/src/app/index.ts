/**
 * Application entry point — exported for ts_binary to bundle.
 *
 * Re-exports the router (the core artifact of a TanStack Router app).
 * In a real app, this would also render the RouterProvider.
 */
export { router } from "../lib";
export type { AppRouter } from "../lib";
