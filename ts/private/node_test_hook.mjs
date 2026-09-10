import { statSync } from "node:fs";
import module from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

if (typeof module.registerHooks !== "function") {
  throw new Error(
    "ts_test //ts/runners:node_test: node:module.registerHooks is " +
      `unavailable on Node ${process.version}, so the package's code cannot ` +
      "resolve at its runfiles paths. Upgrade the js_runtime toolchain.",
  );
}

// The launcher names the importer chain in NODE_PATH, which ESM resolution
// ignores; a bare specifier resolves from each importer's directory in turn.
const trees = (process.env.NODE_PATH ?? "")
  .split(path.delimiter)
  .filter(Boolean)
  .map((dir) => ({
    parentURL: pathToFileURL(path.dirname(dir) + path.sep).href,
  }));

const COMPILED = {
  ".ts": [".js"],
  ".tsx": [".js", ".jsx"],
  ".mts": [".mjs"],
  ".cts": [".cjs"],
};

// The compiled files a `.ts` specifier names, relative or bare: a subpath
// into a workspace member's view holds them too.
function compiledExtensionForms(specifier) {
  const ext = path.extname(specifier);
  if (!(ext in COMPILED)) return [];
  return COMPILED[ext].map((c) => specifier.slice(0, -ext.length) + c);
}

// The files the compiled tree holds for a specifier tsc resolved under the
// tsconfig: a .ts's sibling; the .js or index.js of an extensionless one.
function compiledForms(specifier) {
  const forms = compiledExtensionForms(specifier);
  if (forms.length > 0 || path.extname(specifier) !== "") return forms;
  return [`${specifier}.js`, `${specifier}/index.js`];
}

function firstResolved(forms, attempt) {
  for (const form of forms) {
    try {
      return attempt(form);
    } catch {
      // The specifier as written decides.
    }
  }
  return undefined;
}

function isFile(url) {
  return statSync(url, { throwIfNoEntry: false })?.isFile() ?? false;
}

// A store file's realpath and a member's store path hold the segment; a test
// file's runfiles path does not.
function insideATree(parentURL) {
  const segment = `${path.sep}node_modules${path.sep}`;
  return fileURLToPath(parentURL).includes(segment);
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
    const asWritten = (form) => next(form, context);
    if (!parentURL?.startsWith("file:") || insideATree(parentURL)) {
      return (
        firstResolved(compiledExtensionForms(specifier), asWritten) ??
        asWritten(specifier)
      );
    }
    if (specifier.startsWith("./") || specifier.startsWith("../")) {
      for (const form of [...compiledForms(specifier), specifier]) {
        const url = new URL(form, parentURL);
        if (isFile(url)) return { url: url.href, shortCircuit: true };
      }
      return asWritten(specifier);
    }
    // require() reads NODE_PATH itself and ignores a swapped parentURL.
    const imported = !context.conditions.includes("require");
    if (trees.length > 0 && isBare(specifier)) {
      const fromTrees = (form) => fromTheTrees(form, context, next);
      const attempt = imported ? fromTrees : asWritten;
      return (
        firstResolved(compiledExtensionForms(specifier), attempt) ??
        attempt(specifier)
      );
    }
    return asWritten(specifier);
  },
});
