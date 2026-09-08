import { realpathSync, statSync } from "node:fs";
import module from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

if (typeof module.registerHooks !== "function") {
  throw new Error(
    `ts_test runner "node:test": node:module.registerHooks is unavailable on ` +
      `Node ${process.version}, so the package's code cannot resolve at its ` +
      "runfiles paths. Upgrade the js_runtime toolchain.",
  );
}

// The launcher names the test's node_modules tree in NODE_PATH, which ESM
// resolution ignores; a bare specifier from the package's code resolves there.
const trees = (process.env.NODE_PATH ?? "")
  .split(path.delimiter)
  .filter(Boolean)
  .map((dir) => ({
    realpath: realpathSync(dir) + path.sep,
    parentURL: pathToFileURL(path.dirname(dir) + path.sep).href,
  }));

const COMPILED = {
  ".ts": [".js"],
  ".tsx": [".js", ".jsx"],
  ".mts": [".mjs"],
  ".cts": [".cjs"],
};

// The files the compiled tree holds for a specifier tsc resolved under the
// tsconfig: a .ts's sibling; the .js or index.js of an extensionless one.
function compiledForms(specifier) {
  const ext = path.extname(specifier);
  if (ext in COMPILED) {
    return COMPILED[ext].map((c) => specifier.slice(0, -ext.length) + c);
  }
  return ext === "" ? [`${specifier}.js`, `${specifier}/index.js`] : [];
}

function isFile(url) {
  return statSync(url, { throwIfNoEntry: false })?.isFile() ?? false;
}

function insideATree(parentURL) {
  const importer = fileURLToPath(parentURL);
  return trees.some((tree) => importer.startsWith(tree.realpath));
}

function isBare(specifier) {
  return (
    !specifier.startsWith("/") &&
    !specifier.startsWith("#") &&
    !module.isBuiltin(specifier) &&
    !URL.canParse(specifier)
  );
}

function fromTheTrees(specifier, context, next) {
  let notFound;
  for (const tree of trees) {
    try {
      return next(specifier, { ...context, parentURL: tree.parentURL });
    } catch (error) {
      if (error?.code !== "ERR_MODULE_NOT_FOUND") throw error;
      notFound = error;
    }
  }
  throw notFound;
}

module.registerHooks({
  resolve(specifier, context, next) {
    const { parentURL } = context;
    if (!parentURL?.startsWith("file:") || insideATree(parentURL)) {
      return next(specifier, context);
    }
    if (specifier.startsWith("./") || specifier.startsWith("../")) {
      for (const form of [...compiledForms(specifier), specifier]) {
        const url = new URL(form, parentURL);
        if (isFile(url)) return { url: url.href, shortCircuit: true };
      }
      return next(specifier, context);
    }
    if (trees.length > 0 && isBare(specifier)) {
      return fromTheTrees(specifier, context, next);
    }
    return next(specifier, context);
  },
});
