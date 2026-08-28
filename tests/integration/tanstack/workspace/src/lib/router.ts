import { createRouter } from "@tanstack/react-router";
import type { AnyRouter } from "@tanstack/react-router";
import { routeTree } from "../routes/routeTree.gen";

export type AppRouter = AnyRouter;

export function getRouter(): AppRouter {
  return createRouter({ routeTree });
}
