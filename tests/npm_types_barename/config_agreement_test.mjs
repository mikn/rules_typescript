/**
 * config_agreement_test.mjs — the build's `paths` and the editor's, compared.
 *
 *   node config_agreement_test.mjs <build tsconfig> <editor tsconfig>
 *
 * Two code paths generate these from one build graph: ts_compile writes the
 * first for tsgo, and tsconfig_aspect writes the second for tsserver. Nothing
 * makes them agree, and when they do not the same file type-checks clean in one
 * and shows errors in the other -- which is how the editor came to skip
 * @types/* packages entirely while the build resolved them.
 *
 * The subject is the npm half of each map: the keys whose answer is an npm
 * declaration, which each config reaches by its own route -- an external
 * repository for the build, the installed tree under npm_dir for the editor. So
 * the keys are compared and the values only classified.
 *
 * A key spelled `@types/x` is dropped from both sides. It is the one key the
 * two are meant to disagree about: the build emits it as a by-product of naming
 * every package it resolves, and no import writes it, so the editor's map has
 * nothing to gain from carrying it.
 */

import fs from 'node:fs';

const [buildPath, editorPath] = process.argv.slice(2);

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
 * The keys whose first value is an npm declaration, by the marker that config's
 * route to one leaves in the path.
 *
 * @param {Record<string, string[]>} paths
 * @param {string} marker
 * @returns {string[]}
 */
const npmKeys = (paths, marker) =>
  Object.keys(paths)
    .filter((key) => !key.startsWith('@types/'))
    .filter((key) => (paths[key] || []).some((value) => value.includes(marker)))
    .sort();

const build = npmKeys(pathsOf(buildPath), '/external/+npm+');
const editor = npmKeys(pathsOf(editorPath), '/.bazel/npm/');

if (build.length === 0) fail(`${buildPath} names no npm package at all`);

const missing = build.filter((key) => !editor.includes(key));
const extra = editor.filter((key) => !build.includes(key));

if (missing.length) fail(`the editor resolves neither of: ${missing.join(', ')}`);
if (extra.length) fail(`the editor resolves what the build does not: ${extra.join(', ')}`);

process.stdout.write(`build:  ${build.join(', ')}\n`);
process.stdout.write(`editor: ${editor.join(', ')}\n`);

if (failures.length) {
  for (const message of failures) process.stderr.write(`FAIL: ${message}\n`);
  process.exit(1);
}
process.stdout.write('PASS: the two configs resolve the same npm specifiers\n');
