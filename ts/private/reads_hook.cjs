"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { fileURLToPath } = require("node:url");

const file = process.env.TS_TEST_READS_FILE;
const root = process.env.TS_TEST_READS_ROOT;
const runfiles = process.env.TS_TEST_READS_RUNFILES;

// The first process is vitest's own, whose walk up for a workspace marker is
// not a test's read; the tests run in the processes and threads below it.
if (file && root && process.env.TS_TEST_READS_RUNNER === undefined) {
  process.env.TS_TEST_READS_RUNNER = String(process.pid);
} else if (file && root) {
  install();
}

function install() {
  const realpath = fs.realpathSync.native;
  const append = fs.appendFileSync;
  const seen = new Set();
  const trees = new Set(runfiles ? [runfiles] : []);
  for (const tree of [...trees]) {
    try {
      trees.add(realpath(tree));
    } catch {}
  }
  const under = (p, dir) => p === dir || p.startsWith(dir + path.sep);

  const record = (arg) => {
    let p;
    if (typeof arg === "string") p = arg;
    else if (Buffer.isBuffer(arg)) p = arg.toString();
    else if (arg instanceof URL) {
      try {
        p = fileURLToPath(arg);
      } catch {
        return;
      }
    } else return;
    p = path.resolve(p);
    if (seen.has(p)) return;
    seen.add(p);
    for (const tree of trees) if (under(p, tree)) return;
    let real;
    try {
      real = realpath(p);
    } catch {
      return;
    }
    if (real === root || !under(real, root)) return;
    append(file, real.slice(root.length + 1) + "\n");
  };

  const wrap = (target, name) => {
    const original = target[name];
    if (typeof original !== "function") return;
    const wrapped = function (...args) {
      record(args[0]);
      return original.apply(this, args);
    };
    Object.assign(wrapped, original);
    target[name] = wrapped;
  };
  for (const name of [
    "access",
    "accessSync",
    "copyFile",
    "copyFileSync",
    "createReadStream",
    "exists",
    "existsSync",
    "lstat",
    "lstatSync",
    "open",
    "openSync",
    "opendir",
    "opendirSync",
    "readdir",
    "readdirSync",
    "readFile",
    "readFileSync",
    "readlink",
    "readlinkSync",
    "stat",
    "statSync",
  ]) {
    wrap(fs, name);
  }
  for (const name of [
    "access",
    "copyFile",
    "lstat",
    "open",
    "opendir",
    "readdir",
    "readFile",
    "readlink",
    "stat",
  ]) {
    wrap(fs.promises, name);
  }
  require("node:module").syncBuiltinESMExports();
}
