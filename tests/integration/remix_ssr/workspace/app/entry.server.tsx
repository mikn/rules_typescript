import type { AppLoadContext, EntryContext } from "@remix-run/node";
import { RemixServer } from "@remix-run/react";
import { renderToString } from "react-dom/server";

// Remix's own default node entry imports `isbot` to pick between
// renderToPipeableStream and renderToString; isbot is not a dependency of
// @remix-run/dev, so a build that falls back to that default cannot resolve it.
export default function handleRequest(
  request: Request,
  responseStatusCode: number,
  responseHeaders: Headers,
  remixContext: EntryContext,
  _loadContext?: AppLoadContext,
): Response {
  const html = renderToString(
    <RemixServer context={remixContext} url={request.url} />,
  );
  responseHeaders.set("Content-Type", "text/html");
  return new Response("<!DOCTYPE html>" + html, {
    headers: responseHeaders,
    status: responseStatusCode,
  });
}
