/**
 * tsserver_diag_test.mjs — what tools/tsserver-hook.js does to a language
 * service that resolves through ts.resolveModuleName.
 *
 * Run by tests/lsp/test_tsserver_diagnostics.sh, which supplies typescript and
 * zod from the lockfile and pre-populates the hook's cache:
 *   TSSERVER_HOOK_PRELOAD_MAP='{"zod":"<abs>/zod/index.d.ts"}' \
 *   TSSERVER_HOOK_NO_WORKER=1 \
 *   node --require <hook.js> tsserver_diag_test.mjs <zod.d.ts>
 *
 * The claim under test is the one an editor cares about: with the hook loaded,
 * `import { z } from "zod"` type-checks against zod's REAL declarations even
 * though nothing on the module search path leads to them. Three assertions,
 * every one of which fails if the hook stops working:
 *
 *   baseline  the same language service WITHOUT the hook's resolver reports
 *             TS2307 for "zod" -- without this the other two prove nothing,
 *             because ambient resolution would satisfy them on its own.
 *   resolved  with the hook's resolver, the good file has zero diagnostics AND
 *             a bogus member access on `z` is rejected. A stub, an `any`, or a
 *             widened import would pass the first half and fail the second.
 *   direct    ts.resolveModuleName("zod", ...) returns the exact .d.ts path.
 *
 * Why ts.createLanguageService and not the standalone tsserver.js process:
 * because that process is not what this file's subject serves. tsserver.js does
 * reach `./typescript.js` through require, but its language service resolves
 * through its LanguageServiceHost, so replacing the module's resolveModuleName
 * changes nothing it does -- which is what tools/tsserver-plugin.js and
 * :test_tsserver_plugin exist for. The hook's own consumers are tools that call
 * ts.resolveModuleName themselves, and a host that delegates to it, as the
 * `resolved` block below does, is that surface.
 */

import { createRequire } from 'module';
import { existsSync, readFileSync, statSync } from 'fs';

const require = createRequire(import.meta.url);

const [, , zodDts] = process.argv;

if (!zodDts) {
  process.stderr.write('FATAL: usage: tsserver_diag_test.mjs <zod.d.ts>\n');
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
  pass('hook active: ts._bazelPatched === true');
} else {
  process.stderr.write(
    `FATAL: ts._bazelPatched is ${JSON.stringify(ts._bazelPatched)} -- the hook did not patch ` +
      'the typescript module, so nothing below would be testing the hook\n'
  );
  process.exit(1);
}

if (!existsSync(zodDts)) {
  process.stderr.write(`FATAL: zod declarations not on disk: ${zodDts}\n`);
  process.exit(1);
}

const GOOD = '/virtual/good.ts';
const BAD = '/virtual/bad.ts';
const BOGUS_MEMBER = 'definitelyNotAZodMethod';

const virtualFiles = {
  [GOOD]: 'import { z } from "zod";\nexport const s = z.string();\n',
  [BAD]: `import { z } from "zod";\nexport const s = z.${BOGUS_MEMBER}();\n`,
};

function createHost(resolveModuleNames) {
  const snapshots = new Map(
    Object.entries(virtualFiles).map(([name, content]) => [
      name,
      ts.ScriptSnapshot.fromString(content),
    ])
  );

  const readAny = (p) => {
    const snap = snapshots.get(p);
    if (snap) return snap.getText(0, snap.getLength());
    return existsSync(p) ? readFileSync(p, 'utf8') : undefined;
  };

  return {
    getScriptFileNames: () => Object.keys(virtualFiles),
    getScriptVersion: () => '1',
    getScriptSnapshot: (f) =>
      snapshots.get(f) ||
      (existsSync(f) ? ts.ScriptSnapshot.fromString(readFileSync(f, 'utf8')) : undefined),
    getCurrentDirectory: () => '/virtual',
    getCompilationSettings: () => ({
      moduleResolution: ts.ModuleResolutionKind.Bundler,
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2022,
      noEmit: true,
      strict: true,
    }),
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: (p) => snapshots.has(p) || existsSync(p),
    readFile: readAny,
    directoryExists: (p) => {
      try {
        return statSync(p).isDirectory();
      } catch {
        return false;
      }
    },
    getDirectories: () => [],
    realpath: (p) => p,
    resolveModuleNames,
  };
}

function diagnostics(host, fileName) {
  const service = ts.createLanguageService(host, ts.createDocumentRegistry());
  try {
    return service.getSemanticDiagnostics(fileName).map((d) => ({
      code: d.code,
      message: ts.flattenDiagnosticMessageText(d.messageText, ' '),
    }));
  } finally {
    service.dispose();
  }
}

const describe = (list) => JSON.stringify(list);

// ── baseline: no hook resolver, zod is unreachable ───────────────────────────
{
  const baseline = diagnostics(createHost(undefined), GOOD);
  const missingZod = baseline.filter((d) => d.code === 2307 && d.message.includes("'zod'"));
  if (missingZod.length > 0) {
    pass('baseline: standard resolution cannot find "zod"');
  } else {
    fail(
      'baseline: standard resolution cannot find "zod"',
      'no TS2307 for zod, so "zod" is reachable without the hook and the ' +
        `assertions below would prove nothing. diagnostics: ${describe(baseline)}`
    );
  }
}

// ── with the hook's resolver: real declarations, not a stand-in ──────────────
{
  const resolveModuleNames = (moduleNames, containingFile) =>
    moduleNames.map(
      (name) =>
        ts.resolveModuleName(
          name,
          containingFile,
          { moduleResolution: ts.ModuleResolutionKind.Bundler },
          {
            fileExists: (p) =>
              existsSync(p) || Object.prototype.hasOwnProperty.call(virtualFiles, p),
            readFile: (p) =>
              existsSync(p) ? readFileSync(p, 'utf8') : virtualFiles[p],
          }
        ).resolvedModule
    );

  const host = createHost(resolveModuleNames);

  const good = diagnostics(host, GOOD);
  if (good.length === 0) {
    pass('hook resolver: `import { z } from "zod"` type-checks clean');
  } else {
    fail('hook resolver: `import { z } from "zod"` type-checks clean', describe(good));
  }

  const bad = diagnostics(host, BAD);
  if (bad.some((d) => d.message.includes(BOGUS_MEMBER))) {
    pass(`hook resolver: z.${BOGUS_MEMBER}() is rejected (real declarations loaded)`);
  } else {
    fail(
      `hook resolver: z.${BOGUS_MEMBER}() is rejected`,
      'a nonexistent member on `z` produced no error, so zod resolved to ' +
        `something untyped rather than its own declarations. diagnostics: ${describe(bad)}`
    );
  }
}

// ── the patched resolver returns the exact path it was given ─────────────────
{
  const result = ts.resolveModuleName(
    'zod',
    GOOD,
    { moduleResolution: ts.ModuleResolutionKind.Bundler },
    {
      fileExists: (p) => existsSync(p),
      readFile: (p) => (existsSync(p) ? readFileSync(p, 'utf8') : undefined),
    }
  );
  const resolved = result.resolvedModule && result.resolvedModule.resolvedFileName;
  if (resolved === zodDts) {
    pass(`ts.resolveModuleName("zod") -> ${resolved}`);
  } else {
    fail('ts.resolveModuleName("zod")', `got ${JSON.stringify(resolved)}, want ${zodDts}`);
  }
}

if (failures > 0) {
  process.stderr.write(`\n${failures} FAILED\n`);
  process.exit(1);
}
process.stdout.write('\nALL PASSED\n');
