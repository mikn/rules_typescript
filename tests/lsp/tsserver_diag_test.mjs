// A language service resolving through the export ts.resolveModuleName, with
// tsserver-hook.js loaded. tsserver's host bypasses it: the plugin's test.

import { createRequire } from 'module';
import { existsSync, readFileSync, statSync } from 'fs';

const require = createRequire(import.meta.url);

const [, , libDts] = process.argv;

if (!libDts) {
  process.stderr.write('FATAL: usage: tsserver_diag_test.mjs <lib.d.ts>\n');
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

if (!existsSync(libDts)) {
  process.stderr.write(`FATAL: declarations not on disk: ${libDts}\n`);
  process.exit(1);
}

const PKG = 'src/lib';
const GOOD = '/virtual/good.ts';
const BAD = '/virtual/bad.ts';
const BOGUS_MEMBER = 'definitelyNotAMethod';

const virtualFiles = {
  [GOOD]: `import * as lib from "${PKG}";\nexport const s: number = lib.add(1, 2);\n`,
  [BAD]: `import * as lib from "${PKG}";\nexport const s = lib.${BOGUS_MEMBER}();\n`,
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

// ── baseline: no hook resolver, the package is unreachable ───────────────────
{
  const baseline = diagnostics(createHost(undefined), GOOD);
  const missing = baseline.filter((d) => d.code === 2307 && d.message.includes(`'${PKG}'`));
  if (missing.length > 0) {
    pass(`baseline: standard resolution cannot find "${PKG}"`);
  } else {
    fail(
      `baseline: standard resolution cannot find "${PKG}"`,
      `no TS2307 for ${PKG}, so it is reachable without the hook and the ` +
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
    pass(`hook resolver: \`import * as lib from "${PKG}"\` type-checks clean`);
  } else {
    fail(`hook resolver: \`import * as lib from "${PKG}"\` type-checks clean`, describe(good));
  }

  const bad = diagnostics(host, BAD);
  if (bad.some((d) => d.message.includes(BOGUS_MEMBER))) {
    pass(`hook resolver: lib.${BOGUS_MEMBER}() is rejected (the map's declarations loaded)`);
  } else {
    fail(
      `hook resolver: lib.${BOGUS_MEMBER}() is rejected`,
      'a nonexistent member on `lib` produced no error, so the package resolved to ' +
        `something untyped rather than the declarations the map names. diagnostics: ${describe(bad)}`
    );
  }
}

// ── the patched resolver returns the exact path it was given ─────────────────
{
  const result = ts.resolveModuleName(
    PKG,
    GOOD,
    { moduleResolution: ts.ModuleResolutionKind.Bundler },
    {
      fileExists: (p) => existsSync(p),
      readFile: (p) => (existsSync(p) ? readFileSync(p, 'utf8') : undefined),
    }
  );
  const resolved = result.resolvedModule && result.resolvedModule.resolvedFileName;
  if (resolved === libDts) {
    pass(`ts.resolveModuleName("${PKG}") -> ${resolved}`);
  } else {
    fail(`ts.resolveModuleName("${PKG}")`, `got ${JSON.stringify(resolved)}, want ${libDts}`);
  }
}

if (failures > 0) {
  process.stderr.write(`\n${failures} FAILED\n`);
  process.exit(1);
}
process.stdout.write('\nALL PASSED\n');
