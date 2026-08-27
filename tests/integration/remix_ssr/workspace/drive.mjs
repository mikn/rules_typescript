import { createRequestHandler } from "@remix-run/node";
import * as build from "./server/index.js";

const handler = createRequestHandler(build, "production");
const report = [];

async function probe(name, method, url, form) {
  const init = { method };
  if (form) {
    init.body = new URLSearchParams(form);
    init.headers = { "Content-Type": "application/x-www-form-urlencoded" };
  }
  const response = await handler(new Request("http://acme.test" + url, init));
  const body = await response.text();
  report.push(
    "=== " + name + " ===",
    "status: " + response.status,
    "content-type: " + (response.headers.get("content-type") ?? ""),
    "body:",
    body,
    "=== end " + name + " ===",
  );
}

await probe("get_index", "GET", "/");
await probe("get_index_data", "GET", "/?_data=routes%2F_index");
await probe("get_dash_settings", "GET", "/dash/settings");
await probe("post_dash_settings", "POST", "/dash/settings", {
  field: "acme-posted-value",
});
await probe(
  "post_dash_settings_data",
  "POST",
  "/dash/settings?_data=routes%2Fdash.settings",
  { field: "acme-posted-value" },
);
await probe("get_resource", "GET", "/api/health");
await probe("get_panel", "GET", "/panel");

process.stdout.write(report.join("\n") + "\n");
