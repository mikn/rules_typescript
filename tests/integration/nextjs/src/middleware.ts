import { NextResponse } from "next/server";

export function middleware(): NextResponse {
  const response = NextResponse.next();
  response.headers.set("x-fixture-middleware", "MIDDLEWARE_MARKER");
  return response;
}

export const config = { matcher: ["/dynamic", "/ssr"] };
