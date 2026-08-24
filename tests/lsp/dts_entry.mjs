/**
 * dts_entry.mjs — print {package: absolute .d.ts} for packages in a
 * node_modules tree, as a JSON object suitable for TSSERVER_HOOK_PRELOAD_MAP.
 *
 * Usage: node dts_entry.mjs <node_modules_dir> <package>...
 *
 * Exits non-zero naming the package if its declaration entry point is missing,
 * so a test that needs a real .d.ts cannot silently proceed without one.
 */

import { existsSync, readFileSync } from 'node:fs';
import { isAbsolute, resolve } from 'node:path';

const [, , treeDir, ...packages] = process.argv;

if (!treeDir || packages.length === 0) {
  process.stderr.write('usage: dts_entry.mjs <node_modules_dir> <package>...\n');
  process.exit(2);
}

function isDts(p) {
  return p.endsWith('.d.ts') || p.endsWith('.d.mts') || p.endsWith('.d.cts');
}

// The same order of authority the tsserver hook's worker uses: a package's own
// exports map wins, then its top-level types field, then index.d.ts.
function candidates(pkgJson) {
  const out = [];
  const root = pkgJson.exports && pkgJson.exports['.'];
  if (typeof root === 'string') {
    out.push(root);
  } else if (root && typeof root === 'object') {
    for (const key of ['types', 'typings']) {
      if (typeof root[key] === 'string') out.push(root[key]);
    }
    for (const nested of ['import', 'require', 'default']) {
      const inner = root[nested];
      if (typeof inner === 'string') out.push(inner);
      else if (inner && typeof inner === 'object') {
        for (const key of ['types', 'typings', 'default']) {
          if (typeof inner[key] === 'string') out.push(inner[key]);
        }
      }
    }
  }
  if (typeof pkgJson.types === 'string') out.push(pkgJson.types);
  if (typeof pkgJson.typings === 'string') out.push(pkgJson.typings);
  out.push('./index.d.ts');
  return out;
}

const root = isAbsolute(treeDir) ? treeDir : resolve(process.cwd(), treeDir);
const map = {};
const missing = [];

for (const pkg of packages) {
  const pkgDir = resolve(root, pkg);
  const pkgJsonPath = resolve(pkgDir, 'package.json');
  if (!existsSync(pkgJsonPath)) {
    missing.push(`${pkg}: no ${pkgJsonPath}`);
    continue;
  }
  const pkgJson = JSON.parse(readFileSync(pkgJsonPath, 'utf8'));
  const hit = candidates(pkgJson)
    .map((rel) => resolve(pkgDir, rel))
    .find((abs) => isDts(abs) && existsSync(abs));
  if (hit) map[pkg] = hit;
  else missing.push(`${pkg}: no declaration entry point under ${pkgDir}`);
}

if (missing.length > 0) {
  process.stderr.write(`dts_entry.mjs: ${missing.join('\n')}\n`);
  process.exit(1);
}

process.stdout.write(JSON.stringify(map) + '\n');
