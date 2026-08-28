/**
 * tsserver_plugin_test.mjs — the gold test for tools/tsserver-plugin.js.
 *
 * Run by tests/lsp/test_tsserver_plugin.sh, which stages what
 * `bazel run //:refresh_tsconfig` installs into a scratch workspace and writes
 * the fixture package this drives tsserver over:
 *   node tsserver_plugin_test.mjs <tsserver.js> <workspace_root>
 *
 * The subject is a real tsserver process, spoken to over its own JSON protocol
 * -- not a language service this file assembled. That distinction is the whole
 * point: tsserver resolves through its LanguageServiceHost, so a mechanism that
 * only reaches ts.resolveModuleName is invisible here. Five assertions:
 *
 *   installed  the plugin package is where refresh_tsconfig's manifest says.
 *              tsserver logs and ignores a plugin it cannot load, so without
 *              this a broken emission would look like an unresolved import.
 *   baseline   tsserver WITHOUT the plugin reports TS2307 for "zod" -- nothing
 *              on the fixture's module search path leads to zod's declarations,
 *              so the assertions below are attributable to the plugin.
 *   resolved   with the plugin, `import { z } from "zod"` reaches zero
 *              diagnostics. The map arrives from the worker thread, so this
 *              polls until the deadline rather than asking once.
 *   vscode     the same, with the plugin named in the fixture's tsconfig and NO
 *              --globalPlugins -- the one path VS Code has, since it passes
 *              only a probe location.
 *   real       a bogus member on `z` is still rejected, and the type it is
 *              rejected against comes from the declarations Bazel installed. A
 *              stub, an `any`, or a widened import passes `resolved` and fails
 *              this.
 */

import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';

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
const BOGUS_MEMBER = 'definitelyNotAZodMethod';
const ZOD_DECLARATIONS = join('.bazel', 'npm', 'zod');
const DEADLINE_MS = 60000;

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

const missesZod = (diagnostics) =>
  diagnostics.some((d) => d.code === 2307 && d.text.includes("'zod'"));

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
      if (missesZod(diagnostics)) {
        pass('baseline: tsserver without the plugin cannot find "zod"');
      } else {
        fail(
          'baseline: tsserver without the plugin cannot find "zod"',
          `no TS2307 for zod, so the fixture resolves it without the plugin and the ` +
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
        pass('resolved: `import { z } from "zod"` type-checks clean in tsserver');
      } else {
        fail(
          'resolved: `import { z } from "zod"` type-checks clean in tsserver',
          `${describe(good)}. tsserver stderr: ${server.stderr() || '(empty)'}`
        );
      }

      const bad = await settle(server, BAD, (d) => !missesZod(d));
      const rejection = bad.find((d) => d.text.includes(BOGUS_MEMBER));
      if (!rejection) {
        fail(
          `real: z.${BOGUS_MEMBER}() is rejected`,
          'a nonexistent member on `z` produced no error, so "zod" resolved to ' +
            `something untyped rather than to its own declarations. diagnostics: ${describe(bad)}`
        );
      } else if (!rejection.text.includes(ZOD_DECLARATIONS)) {
        fail(
          `real: z.${BOGUS_MEMBER}() is rejected against the installed declarations`,
          `the rejection does not name ${ZOD_DECLARATIONS}, so it came from somewhere ` +
            `other than what refresh_tsconfig installed: ${rejection.text}`
        );
      } else {
        pass(`real: z.${BOGUS_MEMBER}() is rejected against ${ZOD_DECLARATIONS}`);
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
