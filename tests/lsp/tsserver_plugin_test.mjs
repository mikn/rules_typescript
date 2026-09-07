// A real tsserver process over its own protocol: tsserver resolves through its
// LanguageServiceHost, and a hook on ts.resolveModuleName is invisible to it.

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

const [, , tsserverJs, workspaceRoot] = process.argv;

if (!tsserverJs || !workspaceRoot) {
  process.stderr.write('FATAL: usage: tsserver_plugin_test.mjs <tsserver.js> <workspace_root>\n');
  process.exit(2);
}

const PLUGIN_NAME = '@rules_typescript/tsserver-plugin';
const PLUGIN_DIR = join(workspaceRoot, '.bazel/node_modules', PLUGIN_NAME);
const PROBE_DIR = join(workspaceRoot, '.bazel');
const GOOD = join(workspaceRoot, 'fixture/src/good.ts');
const BAD = join(workspaceRoot, 'fixture/src/bad.ts');
const BOGUS_MEMBER = 'definitelyNotAMethod';
const DEADLINE_MS = 60000;

// The first-party package the fixture imports: the first one the staged hook
// data names, so the test follows the graph rather than pinning a label.
const hookData = JSON.parse(
  readFileSync(join(workspaceRoot, '.bazel/tsserver-hook-data.json'), 'utf8')
);
const PKG = (hookData.packages || [])[0];
if (!PKG) {
  process.stderr.write('FATAL: the staged hook data names no package\n');
  process.exit(1);
}
const LIB_DECLARATIONS = join('bazel-bin', PKG);

function write(rel, contents) {
  const p = join(workspaceRoot, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, contents);
}

// What a build leaves for the worker: the .d.ts in bazel-bin wins over a source.
write(`${LIB_DECLARATIONS}/index.d.ts`, 'export declare function add(a: number, b: number): number;\n');

// The fixture's own tsconfig, with no `paths`: tsserver builds these files'
// program from it, so the package is reachable only through the plugin it names.
write(
  'fixture/tsconfig.json',
  JSON.stringify(
    {
      compilerOptions: {
        target: 'ES2022',
        module: 'Preserve',
        moduleResolution: 'Bundler',
        strict: true,
        noEmit: true,
        skipLibCheck: true,
        plugins: [{ name: PLUGIN_NAME }],
      },
      include: ['src'],
    },
    null,
    2
  ) + '\n'
);
write('fixture/src/good.ts', `import * as lib from "${PKG}";\nexport const s: number = lib.add(1, 2);\n`);
write('fixture/src/bad.ts', `import * as lib from "${PKG}";\nexport const s = lib.${BOGUS_MEMBER}();\n`);

let failures = 0;

function pass(name) {
  process.stdout.write(`PASS: ${name}\n`);
}

function fail(name, detail) {
  process.stderr.write(`FAIL: ${name}${detail ? ': ' + detail : ''}\n`);
  failures += 1;
}

const describe = (diagnostics) =>
  diagnostics.length === 0
    ? '(clean)'
    : JSON.stringify(diagnostics.map((d) => `TS${d.code} ${d.text}`));

/** A tsserver process, driven over stdin/stdout with its line-delimited JSON. */
function startServer({ plugin }) {
  const args = [tsserverJs, '--disableAutomaticTypingAcquisition'];
  if (plugin === 'global') {
    args.push('--globalPlugins', PLUGIN_NAME, '--pluginProbeLocations', PROBE_DIR);
  } else if (plugin === 'tsconfig') {
    // What VS Code passes: the probe location alone. The plugin is named in
    // the fixture's tsconfig, which is where the generator now puts it.
    args.push('--pluginProbeLocations', PROBE_DIR);
  }

  const proc = spawn(process.execPath, args, {
    cwd: workspaceRoot,
    stdio: ['pipe', 'pipe', 'pipe'],
  });

  let stderr = '';
  proc.stderr.on('data', (chunk) => (stderr += chunk));

  const waiters = [];
  let buffered = '';
  proc.stdout.on('data', (chunk) => {
    buffered += chunk;
    for (let nl = buffered.indexOf('\n'); nl !== -1; nl = buffered.indexOf('\n')) {
      const line = buffered.slice(0, nl).trim();
      buffered = buffered.slice(nl + 1);
      if (!line.startsWith('{')) continue;
      let message;
      try {
        message = JSON.parse(line);
      } catch {
        continue;
      }
      for (let i = waiters.length - 1; i >= 0; i -= 1) {
        if (waiters[i].seq === message.request_seq) waiters.splice(i, 1)[0].resolve(message);
      }
    }
  });

  let seq = 0;

  function send(command, args) {
    seq += 1;
    proc.stdin.write(JSON.stringify({ seq, type: 'request', command, arguments: args }) + '\n');
    return seq;
  }

  return {
    stderr: () => stderr,
    open(file) {
      send('open', { file });
    },
    diagnostics(file) {
      const wanted = send('semanticDiagnosticsSync', { file });
      return new Promise((resolve, reject) => {
        waiters.push({ seq: wanted, resolve: (m) => resolve(m.body || []) });
        setTimeout(() => reject(new Error(`tsserver did not answer for ${file}`)), 30000).unref();
      });
    },
    stop() {
      proc.kill();
    },
  };
}

/**
 * Poll until `accept` is satisfied, then return those diagnostics.
 *
 * The resolution map is built off-thread, so an unresolved first answer is the
 * documented behaviour rather than a failure; only the deadline is one.
 */
async function settle(server, file, accept) {
  const until = Date.now() + DEADLINE_MS;
  let last = await server.diagnostics(file);
  while (!accept(last) && Date.now() < until) {
    await new Promise((r) => setTimeout(r, 250));
    last = await server.diagnostics(file);
  }
  return last;
}

const missesPackage = (diagnostics) =>
  diagnostics.some((d) => d.code === 2307 && d.text.includes(`'${PKG}'`));

async function main() {
  for (const file of ['index.js', 'package.json', 'tsserver-hook-resolver.js', 'tsserver-hook-worker.js']) {
    if (!existsSync(join(PLUGIN_DIR, file))) {
      fail(
        'installed: refresh_tsconfig installs the plugin package',
        `${join(PLUGIN_DIR, file)} is missing -- does ts_refresh_tsconfig still ` +
          'copy it to .bazel/node_modules/@rules_typescript/tsserver-plugin?'
      );
      process.exit(1);
    }
  }
  pass('installed: refresh_tsconfig installs the plugin package');

  {
    const server = startServer({ plugin: false });
    try {
      server.open(GOOD);
      const diagnostics = await server.diagnostics(GOOD);
      if (missesPackage(diagnostics)) {
        pass(`baseline: tsserver without the plugin cannot find "${PKG}"`);
      } else {
        fail(
          `baseline: tsserver without the plugin cannot find "${PKG}"`,
          `no TS2307 for ${PKG}, so the fixture resolves it without the plugin and the ` +
            `assertions below would prove nothing. diagnostics: ${describe(diagnostics)}`
        );
      }
    } finally {
      server.stop();
    }
  }

  {
    const server = startServer({ plugin: 'global' });
    try {
      server.open(GOOD);
      server.open(BAD);

      const good = await settle(server, GOOD, (d) => d.length === 0);
      if (good.length === 0) {
        pass(`resolved: \`import * as lib from "${PKG}"\` type-checks clean in tsserver`);
      } else {
        fail(
          `resolved: \`import * as lib from "${PKG}"\` type-checks clean in tsserver`,
          `${describe(good)}. tsserver stderr: ${server.stderr() || '(empty)'}`
        );
      }

      const bad = await settle(server, BAD, (d) => !missesPackage(d));
      const rejection = bad.find((d) => d.text.includes(BOGUS_MEMBER));
      if (!rejection) {
        fail(
          `real: lib.${BOGUS_MEMBER}() is rejected`,
          `a nonexistent member on \`lib\` produced no error, so "${PKG}" resolved to ` +
            `something untyped rather than to its declarations. diagnostics: ${describe(bad)}`
        );
      } else if (!rejection.text.includes(LIB_DECLARATIONS)) {
        fail(
          `real: lib.${BOGUS_MEMBER}() is rejected against the bazel-bin declarations`,
          `the rejection does not name ${LIB_DECLARATIONS}, so it came from somewhere ` +
            `other than the .d.ts the map names: ${rejection.text}`
        );
      } else {
        pass(`real: lib.${BOGUS_MEMBER}() is rejected against ${LIB_DECLARATIONS}`);
      }
    } finally {
      server.stop();
    }
  }

  {
    const server = startServer({ plugin: 'tsconfig' });
    try {
      server.open(GOOD);
      const good = await settle(server, GOOD, (d) => d.length === 0);
      if (good.length === 0) {
        pass('vscode: a probe location alone loads the plugin named in tsconfig');
      } else {
        fail(
          'vscode: a probe location alone loads the plugin named in tsconfig',
          `${describe(good)} -- tsserver logs and ignores a plugin it cannot load, ` +
            'so this is what an editor that passes no --globalPlugins sees. ' +
            `tsserver stderr: ${server.stderr() || '(empty)'}`
        );
      }
    } finally {
      server.stop();
    }
  }

  if (failures > 0) {
    process.stderr.write(`\n${failures} FAILED\n`);
    process.exit(1);
  }
  process.stdout.write('\nALL PASSED\n');
}

main().catch((e) => {
  process.stderr.write(`FATAL: ${e.stack || e.message}\n`);
  process.exit(1);
});
