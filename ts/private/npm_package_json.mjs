// Emits the package.json of a ts_npm_publish target: the template, with the
// fields listed in the patch file applied.
//
//   node npm_package_json.mjs <template.json> <patch.json> <out.json>
//
// The patch is {version?, main?, types?, exports?}. version replaces whatever
// the template has; the entry-point fields are only filled in when the
// template does not declare them.
import { readFileSync, writeFileSync } from "node:fs";

const [, , templatePath, patchPath, outPath] = process.argv;

function readJson(path) {
  const text = readFileSync(path, "utf8");
  try {
    return JSON.parse(text);
  } catch (err) {
    throw new Error(`ts_npm_publish: ${path} is not valid JSON: ${err.message}`);
  }
}

const pkg = readJson(templatePath);
if (pkg === null || typeof pkg !== "object" || Array.isArray(pkg)) {
  throw new Error(
    `ts_npm_publish: ${templatePath} must contain a JSON object, got ${Array.isArray(pkg) ? "an array" : typeof pkg}`,
  );
}
const patch = readJson(patchPath);

if (patch.version) {
  pkg.version = patch.version;
}
for (const field of ["main", "types", "exports"]) {
  if (patch[field] !== undefined && pkg[field] === undefined) {
    pkg[field] = patch[field];
  }
}

writeFileSync(outPath, JSON.stringify(pkg, null, 2) + "\n", "utf8");
