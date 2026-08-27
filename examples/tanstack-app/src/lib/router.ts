import { createRouter } from "@tanstack/react-router";
import type { AnyRouter } from "@tanstack/react-router";
import { routeTree } from "../routes/routeTree.gen";

export type AppRouter = AnyRouter;

// The TanStack Start plugin imports getRouter from the router entry it was
// pointed at, and calls it once per request during SSR.
export function getRouter(): AppRouter {
  return createRouter({ routeTree, scrollRestoration: true });
}
