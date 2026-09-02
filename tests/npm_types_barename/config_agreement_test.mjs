/**
 * config_agreement_test.mjs — the build's `paths` and the editor's, compared.
 *
 *   node config_agreement_test.mjs <mode> <build tsconfig> <editor tsconfig> ...
 *
 * Two code paths generate these from one build graph: ts_compile writes the
 * first for tsgo, and tsconfig_aspect writes the second for tsserver. Nothing
 * makes them agree, and when they do not the same file type-checks clean in one
 * and shows errors in the other -- which is how the editor came to skip
 * @types/* packages entirely while the build resolved them.
 *
 * The subject is the npm half of each map: the keys whose answer is an npm
 * declaration, which each config reaches by its own route -- an external
 * repository for the build, the installed tree under npm_dir for the editor.
 *
 * The two routes cannot be compared as strings, and the difference is not a
 * disagreement: a repository directory carries a name that changes with every
 * version bump, and the installed tree re-homes a paired @types/* package's
 * declarations under the runtime name they answer for (@types/culori's files
 * install at `.bazel/npm/culori`). So a value is split into the package
 * directory it names and the path under it, and two things are asserted:
 *
 *   - the paths under the package are equal, entry for entry and in order.
 *     Comparing key sets alone let `@babel/core/*` resolve into @babel/core's
 *     own repository, which ships no .d.ts at all, while `@babel/core` resolved
 *     into @types/babel__core beside it;
 *   - within one config, a key and its `/*` wildcard name one package. That is
 *     the half the comparison above cannot see, and the half that bug broke.
 *
 * A key spelled `@types/x` is dropped from both sides. It is the one key the
 * two are meant to disagree about: the build emits it as a by-product of naming
 * every package it resolves, and no import writes it, so the editor's map has
 * nothing to gain from carrying it.
 *
 * Mode `keys` adds the key sets to that, and two fixtures ask for it. The two
 * configs name the same set only where the closure holds no package the editor
 * drops: over :mangled_scope's closure the build names 165 npm keys and the
 * editor 105, because the build gives a key to every package it resolves --
 * `debug`, `ms` and `caniuse-lite` among them, none of which ships a .d.ts --
 * and the editor drops those. Measured, pre-existing, and a different question
 * from this one. The `exports` subpaths used to be the other half of that gap
 * and are not any more: :subpath_exports is a closure whose only package
 * declares three of them, and it asks for `keys`.
 */

import fs from 'node:fs';

const failures = [];
const fail = (message) => failures.push(message);

/** @param {string} file @returns {Record<string, string[]>} */
const pathsOf = (file) => {
  const config = JSON.parse(fs.readFileSync(file, 'utf8'));
  const paths = config.compilerOptions && config.compilerOptions.paths;
  if (!paths) fail(`${file} has no compilerOptions.paths`);
  return paths || {};
};

/**
 * A value split at the package directory, or null when it names no npm package.
 *
 * The build's route is an external repository, one path segment. The editor's
 * is the installed tree, where a scoped package is two.
 *
 * @param {string} value
 * @returns {{pkg: string, under: string} | null}
 */
const split = (value) => {
  const build = /\/external\/([^/]+)(?:\/(.*))?$/.exec(value);
  if (build) return { pkg: build[1], under: build[2] || '' };
  const editor = /\.bazel\/npm\/(.*)$/.exec(value);
  if (!editor) return null;
  const segments = editor[1].split('/');
  const depth = segments[0].startsWith('@') ? 2 : 1;
  return { pkg: segments.slice(0, depth).join('/'), under: segments.slice(depth).join('/') };
};

/**
 * The npm keys of one config, each as what it resolves to under its package. A
 * key counts as npm's when every value it holds is an npm value: a `module_name`
 * or `path_alias` key answers with workspace paths and is not this test's
 * business.
 *
 * @param {Record<string, string[]>} paths
 * @returns {Map<string, {pkg: string, under: string}[]>}
 */
const npmKeys = (paths) => {
  const out = new Map();
  for (const [key, values] of Object.entries(paths)) {
    if (key.startsWith('@types/') || !values.length) continue;
    const parts = values.map(split);
    if (parts.some((part) => part === null)) continue;
    out.set(key, parts);
  }
  return out;
};

/** @param {string} label @param {Map<string, {pkg: string}[]>} keys */
const checkOnePackagePerKey = (label, keys) => {
  for (const [key, values] of keys) {
    if (key.endsWith('/*')) continue;
    const wildcard = keys.get(key + '/*') || [];
    const packages = [...new Set([...values, ...wildcard].map((value) => value.pkg))];
    if (packages.length > 1) {
      fail(
        `${label} resolves ${key} and ${key}/* into different packages: ` +
          packages.join(' and ')
      );
    }
  }
};

/** @param {{under: string}[]} values */
const under = (values) => values.map((value) => value.under).join(', ');

const compare = (mode, buildPath, editorPath) => {
  const build = npmKeys(pathsOf(buildPath));
  const editor = npmKeys(pathsOf(editorPath));

  if (build.size === 0) fail(`${buildPath} names no npm package at all`);
  if (editor.size === 0) fail(`${editorPath} names no npm package at all`);

  checkOnePackagePerKey('the build', build);
  checkOnePackagePerKey('the editor', editor);

  if (mode === 'keys') {
    const missing = [...build.keys()].filter((key) => !editor.has(key)).sort();
    const extra = [...editor.keys()].filter((key) => !build.has(key)).sort();
    if (missing.length) fail(`the editor resolves neither of: ${missing.join(', ')}`);
    if (extra.length) fail(`the editor resolves what the build does not: ${extra.join(', ')}`);
  }

  for (const key of [...build.keys()].sort()) {
    if (!editor.has(key)) continue;
    if (under(build.get(key)) !== under(editor.get(key))) {
      fail(
        `${key} resolves to a different file in each config: the build to ` +
          `<pkg>/{${under(build.get(key))}}, the editor to <pkg>/{${under(editor.get(key))}}`
      );
    }
  }

  const shared = [...build.keys()].filter((key) => editor.has(key)).length;
  process.stdout.write(
    `${buildPath.split('/').pop()} (${mode}): ${build.size} build keys, ` +
      `${editor.size} editor keys, ${shared} compared by value\n`
  );
};

const args = process.argv.slice(2);
if (args.length < 3 || args.length % 3 !== 0) {
  process.stderr.write('FATAL: expected triples of <mode> <build tsconfig> <editor tsconfig>\n');
  process.exit(1);
}
for (let i = 0; i < args.length; i += 3) {
  if (args[i] !== 'keys' && args[i] !== 'values') {
    process.stderr.write(`FATAL: unknown mode ${args[i]}\n`);
    process.exit(1);
  }
  compare(args[i], args[i + 1], args[i + 2]);
}

if (failures.length) {
  for (const message of failures) process.stderr.write(`FAIL: ${message}\n`);
  process.exit(1);
}
process.stdout.write('PASS: the two configs resolve the same npm specifiers to the same files\n');
