import module from "node:module";

if (typeof module.registerHooks !== "function") {
  throw new Error(
    `ts_test runner "node:test": node:module.registerHooks is unavailable on Node ${process.version}, ` +
      "so a relative TypeScript specifier cannot be resolved. Upgrade the js_runtime toolchain.",
  );
}

// oxc emits a relative `./x.ts` specifier verbatim; the module runs at its
// bazel-out realpath, where only the .js is. A specifier node resolves stands.
module.registerHooks({
  resolve(specifier, context, next) {
    try {
      return next(specifier, context);
    } catch (error) {
      const rewritten = specifier.replace(/\.tsx?$/, ".js");
      if (error?.code !== "ERR_MODULE_NOT_FOUND" || rewritten === specifier) {
        throw error;
      }
      return next(rewritten, context);
    }
  },
});
