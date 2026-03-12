/**
 * React application entry point.
 *
 * Renders the RouterProvider with TanStack Router into the DOM.
 * This file is the entry point for the Vite SPA bundle.
 */

import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "../lib";

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("No #root element found");

createRoot(rootEl).render(<RouterProvider router={router} />);
