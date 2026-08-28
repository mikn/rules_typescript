/**
 * resolve_test.mjs — what the tsserver hook does to ts.resolveModuleName.
 *
 * Run by tests/lsp/test_resolve_integration.sh:
 *   node --require <hook.js> resolve_test.mjs <zod.d.ts> <vitest.d.ts> <alias_dir>
 *
 * Four claims, each of which the hook can break on its own:
 *   - the patch applied at all (ts._bazelPatched)
 *   - a bare npm specifier in the cache resolves to that exact .d.ts
 *   - a # gazelle:ts_path_alias prefix resolves through to a source file
 *   - a specifier in neither falls through to TypeScript's own resolver
 *
 * Every path is supplied by the caller and asserted against, so a stale or
 * missing file fails rather than turning an assertion into a no-op.
 */

import { createRequire } from 'module';
import { existsSync, readFileSync } from 'fs';

const require = createRequire(import.meta.url);

const [, , zodDts, vitestDts, aliasDir] = process.argv;

if (!zodDts || !vitestDts || !aliasDir) {
  process.stderr.write(
    'FATAL: usage: resolve_test.mjs <zod.d.ts> <vitest.d.ts> <alias_dir>\n'
  );
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
  getCurrentDirectory: () => aliasDir,
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

expectResolved('npm specifier "zod"', 'zod', `${aliasDir}/app/main.ts`, zodDts);
expectResolved('npm specifier "vitest"', 'vitest', `${aliasDir}/app/main.ts`, vitestDts);
expectResolved(
  'path alias "@/lib/math"',
  '@/lib/math',
  `${aliasDir}/app/main.ts`,
  `${aliasDir}/lib/math.ts`
);

// Fallthrough: a specifier the cache knows nothing about must reach TypeScript's
// own resolver rather than being answered, or thrown on, by the hook.
{
  const label = 'unknown specifier falls through to the TypeScript resolver';
  try {
    const result = resolve('no-such-package-anywhere', `${aliasDir}/app/main.ts`);
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
