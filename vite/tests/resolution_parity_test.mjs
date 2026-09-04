/**
 * resolution_parity_test.mjs — one module graph, two resolution modes.
 *
 *   node resolution_parity_test.mjs <vite_plugin_bazel.mjs>
 *
 * Serving first-party source in dev and pre-compiled .js in prod means the same
 * import specifier travels two different code paths, and the failure mode of
 * that is "works under `bazel run //:dev`, fails under `vite build`". Two suites
 * that each pass prove nothing about it. So this builds ONE fixture graph on
 * disk -- checked-in source, its bazel-bin output, an npm package -- and asserts
 * that each specifier lands on the same MODULE IDENTITY in both modes:
 * the same workspace-relative path, extension and root stripped.
 *
 * What differs is who transforms it (`precompiled`), which is asserted too.
 *
 * The alias table is the one `ts_dev_server` generates from TsModuleInfo, and it
 * is applied identically in both modes, because `resolve.alias` is honoured by
 * `vite dev` and `vite build` alike. Its shape is load-bearing: an exact-match
 * RegExp for the package plus a string prefix for subpaths, because a string
 * `find` in Vite's alias plugin also matches everything under it.
 */

import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const bundlePath = process.argv[2];
if (!bundlePath || !fs.existsSync(bundlePath)) {
  process.stderr.write(`FATAL: bundle not found: ${bundlePath}\n`);
  process.exit(1);
}

const { BazelResolver } = await import(pathToFileURL(bundlePath).href);

// ── The fixture graph ───────────────────────────────────────────────────────

const root = fs.mkdtempSync(path.join(process.env.TEST_TMPDIR || os.tmpdir(), 'parity-'));
const workspaceRoot = path.join(root, 'ws');
const bazelBin = path.join(workspaceRoot, 'bazel-bin');

const write = (file, content) => {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, content);
};

// Checked-in source.
write(
  path.join(workspaceRoot, 'app/main.ts'),
  'import { help } from "./helper.js";\n' +
    'import { name } from "@fixture/lib";\n' +
    'import leftpad from "leftpad";\n' +
    'export const app = [help, name, leftpad];\n',
);
write(path.join(workspaceRoot, 'app/helper.ts'), 'export const help = "help";\n');
write(path.join(workspaceRoot, 'packages/lib/index.ts'), 'export const name = "@fixture/lib";\n');

// What Bazel compiled from it.
write(path.join(bazelBin, 'app/main.js'), 'export const app = [];\n');
write(path.join(bazelBin, 'app/helper.js'), 'export const help = "help";\n');
write(path.join(bazelBin, 'packages/lib/index.js'), 'export const name = "@fixture/lib";\n');

// The npm tree Bazel built.
write(path.join(workspaceRoot, 'node_modules/leftpad/package.json'), '{"name":"leftpad"}\n');
write(path.join(workspaceRoot, 'node_modules/leftpad/index.js'), 'export default () => "";\n');

// ── The alias table ts_dev_server generates ─────────────────────────────────

const escape = (name) => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const aliases = [
  {
    find: new RegExp('^' + escape('@fixture/lib') + '$'),
    replacement: path.join(workspaceRoot, 'packages/lib/index.ts'),
  },
  { find: '@fixture/lib', replacement: path.join(workspaceRoot, 'packages/lib') },
];

/** Vite's own alias matching: exact, or the prefix followed by a slash. */
function applyAlias(specifier) {
  for (const { find, replacement } of aliases) {
    if (find instanceof RegExp) {
      if (find.test(specifier)) return specifier.replace(find, replacement);
      continue;
    }
    if (specifier === find) return replacement;
    if (specifier.startsWith(find + '/')) return replacement + specifier.slice(find.length);
  }
  return specifier;
}

// ── Resolution, in both modes ───────────────────────────────────────────────

// Each mode's real importer: dev serves the .ts entry, prod links the .js Bazel
// compiled from it.
const importers = {
  serve: path.join(workspaceRoot, 'app/main.ts'),
  build: path.join(bazelBin, 'app/main.js'),
};

function resolve(mode, specifier) {
  const resolver = new BazelResolver({ workspaceRoot, bazelBin, mode });
  return resolver.resolveId(applyAlias(specifier), importers[mode]);
}

/**
 * The module a resolution names, independent of which root it came out of and
 * which extension that root spells it with. Two resolutions with the same
 * identity are the same module of the graph.
 */
function identity(resolution) {
  if (resolution === null) return null;
  const file = resolution.filePath;
  const rel = file.startsWith(bazelBin + path.sep)
    ? path.relative(bazelBin, file)
    : path.relative(workspaceRoot, file);
  return rel.replace(/\.(tsx?|jsx?)$/, '');
}

const tests = [];
const test = (name, fn) => tests.push([name, fn]);

test('a first-party bare specifier is one module in both modes', () => {
  const serve = resolve('serve', '@fixture/lib');
  const build = resolve('build', '@fixture/lib');

  assert.equal(identity(serve), 'packages/lib/index');
  assert.equal(
    identity(build),
    identity(serve),
    `@fixture/lib is ${identity(build)} in prod and ${identity(serve)} in dev`,
  );

  // Same module, different owner: Vite transforms the source, Bazel already
  // transformed the output.
  assert.equal(serve.precompiled, false);
  assert.equal(build.precompiled, true);
  assert.ok(serve.filePath.endsWith('.ts'));
  assert.ok(build.filePath.endsWith('.js'));
});

test('a relative ./foo.js import is one module in both modes', () => {
  const serve = resolve('serve', './helper.js');
  const build = resolve('build', './helper.js');

  // TypeScript's node16 ESM output spells `./helper.ts` as `./helper.js` in the
  // source text, so dev has to invert it and prod does not.
  assert.equal(identity(serve), 'app/helper');
  assert.equal(
    identity(build),
    identity(serve),
    `./helper.js is ${identity(build)} in prod and ${identity(serve)} in dev`,
  );
  assert.equal(serve.precompiled, false);
  assert.equal(build.precompiled, true);
});

test('an npm bare specifier is left to Vite in both modes', () => {
  // Neither mode may capture it, and the alias table must not swallow it: npm
  // resolution is Vite's, out of the node_modules tree Bazel built.
  assert.equal(applyAlias('leftpad'), 'leftpad');
  assert.equal(identity(resolve('serve', 'leftpad')), null);
  assert.equal(identity(resolve('build', 'leftpad')), null);
});

test('a bare specifier that only shares a prefix is not swallowed', () => {
  // A string `find` in Vite's alias plugin matches `find` and `find/...` only,
  // which is why the exact entry is a RegExp: `@fixture/libish` must not become
  // `<pkg>/index.ts` + "ish".
  assert.equal(applyAlias('@fixture/libish'), '@fixture/libish');
  assert.equal(identity(resolve('serve', '@fixture/libish')), null);
  assert.equal(identity(resolve('build', '@fixture/libish')), null);
});

test('a first-party subpath is delegated in both modes', () => {
  // The alias rewrites `@fixture/lib/button` to an extensionless path under the
  // package. Neither mode claims it, so Vite's own extension probing decides --
  // the same probing in dev and in prod.
  assert.equal(applyAlias('@fixture/lib/button'), path.join(workspaceRoot, 'packages/lib/button'));
  assert.equal(identity(resolve('serve', '@fixture/lib/button')), null);
  assert.equal(identity(resolve('build', '@fixture/lib/button')), null);
});

test('a ts_codegen output resolves to bazel-bin in dev, and nowhere else', () => {
  // Generated source has no checked-in counterpart, so it is the one case where
  // dev must still read bazel-bin. Prod links the compiled .js of the same file.
  write(path.join(bazelBin, 'app/routes.gen.ts'), 'export const routes = [];\n');
  write(path.join(bazelBin, 'app/routes.gen.js'), 'export const routes = [];\n');

  const serve = resolve('serve', './routes.gen.js');
  const build = resolve('build', './routes.gen.js');
  assert.equal(identity(serve), 'app/routes.gen');
  assert.equal(identity(build), identity(serve));
  assert.ok(serve.filePath.startsWith(bazelBin + path.sep), 'dev must read generated code from bazel-bin');
});

let failed = 0;
for (const [name, fn] of tests) {
  try {
    await fn();
    process.stdout.write(`PASS: ${name}\n`);
  } catch (err) {
    failed++;
    process.stdout.write(`FAIL: ${name}\n${err && err.stack ? err.stack : err}\n`);
  }
}

process.stdout.write(`\n${tests.length - failed}/${tests.length} passed\n`);
process.exit(failed === 0 ? 0 : 1);
