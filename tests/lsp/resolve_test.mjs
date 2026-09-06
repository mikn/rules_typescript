/**
 * resolve_test.mjs — what the tsserver hook does to ts.resolveModuleName.
 *
 * Run by tests/lsp/test_resolve_integration.sh:
 *   node --require <hook.js> resolve_test.mjs <lib.d.ts> <work_dir>
 *
 * Three claims, each of which the hook can break on its own:
 *   - the patch applied at all (ts._bazelPatched)
 *   - a first-party package in the cache resolves to that exact .d.ts
 *   - a specifier not in the cache falls through to TypeScript's own resolver
 *
 * Every path is supplied by the caller and asserted against, so a stale or
 * missing file fails rather than turning an assertion into a no-op.
 */

import { createRequire } from 'module';
import { existsSync, readFileSync } from 'fs';

const require = createRequire(import.meta.url);

const [, , libDts, workDir] = process.argv;

if (!libDts || !workDir) {
  process.stderr.write('FATAL: usage: resolve_test.mjs <lib.d.ts> <work_dir>\n');
  process.exit(1);
}

let failures = 0;

function pass(name) {
  process.stdout.write(`PASS: ${name}\n`);
}

function fail(name, detail) {
  process.stderr.write(`FAIL: ${name}${detail ? ': ' + detail : ''}\n`);
  failures += 1;
}

let ts;
try {
  ts = require('typescript');
} catch (e) {
  process.stderr.write(`FATAL: cannot load 'typescript' module: ${e.message}\n`);
  process.exit(1);
}

process.stdout.write(`INFO: TypeScript ${ts.version}\n`);

if (ts._bazelPatched === true) {
  pass('hook applied: ts._bazelPatched === true');
} else {
  process.stderr.write(
    `FATAL: ts._bazelPatched is ${JSON.stringify(ts._bazelPatched)} -- the hook did not patch ` +
      'the typescript module\n'
  );
  process.exit(1);
}

const host = {
  fileExists: (p) => existsSync(p),
  readFile: (p) => (existsSync(p) ? readFileSync(p, 'utf8') : undefined),
  getCurrentDirectory: () => workDir,
  getDirectories: () => [],
  useCaseSensitiveFileNames: () => true,
  getCanonicalFileName: (f) => f,
  getNewLine: () => '\n',
};

function resolve(moduleName, containingFile) {
  return ts.resolveModuleName(
    moduleName,
    containingFile,
    { moduleResolution: ts.ModuleResolutionKind.Bundler },
    host
  );
}

function expectResolved(label, moduleName, containingFile, want) {
  const result = resolve(moduleName, containingFile);
  const got = result && result.resolvedModule && result.resolvedModule.resolvedFileName;
  if (got !== want) {
    fail(label, `resolved to ${JSON.stringify(got)}, want ${want}`);
    return;
  }
  if (!existsSync(got)) {
    fail(label, `resolved to a path that is not on disk: ${got}`);
    return;
  }
  pass(`${label} -> ${got}`);
}

expectResolved('package "src/lib"', 'src/lib', `${workDir}/app/main.ts`, libDts);

// Fallthrough: a specifier the cache knows nothing about must reach TypeScript's
// own resolver rather than being answered, or thrown on, by the hook.
{
  const label = 'unknown specifier falls through to the TypeScript resolver';
  try {
    const result = resolve('no-such-package-anywhere', `${workDir}/app/main.ts`);
    if (result && result.resolvedModule) {
      fail(label, `the hook invented a resolution: ${result.resolvedModule.resolvedFileName}`);
    } else {
      pass(label);
    }
  } catch (e) {
    fail(label, `threw ${e.message}`);
  }
}

if (failures > 0) {
  process.stderr.write(`\n${failures} FAILED\n`);
  process.exit(1);
}
process.stdout.write('\nALL PASSED\n');
