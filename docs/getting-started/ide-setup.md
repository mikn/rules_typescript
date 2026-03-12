# IDE Setup

## VS Code and WebStorm

Run once to generate a workspace-root `tsconfig.json` that your IDE uses for code intelligence:

```bash
bazel run //:refresh_tsconfig
```

Re-run whenever you add or remove packages.

**VS Code**: after regenerating, run `TypeScript: Restart TS Server` from the command palette to pick up the new paths.

**WebStorm**: the IDE watches `tsconfig.json` for changes and updates automatically.

## What `refresh_tsconfig` Does

The generated `tsconfig.json` at the repo root contains:

- `paths` entries mapping every `ts_compile` target's package name to its source directory
- `rootDirs` that include both source directories and Bazel output directories
- `moduleResolution: "Bundler"` matching the build-time tsconfig

This allows your IDE to resolve cross-package imports (`import { Button } from "//packages/ui"`) without running Bazel.

## Debugging Tests in VS Code

To attach a debugger to vitest running inside the Bazel sandbox:

**Step 1.** Add a debug target to your `BUILD.bazel`:

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    node_modules = ":node_modules",
    tags = ["manual"],  # exclude from bazel test //...
    env = {
        "NODE_OPTIONS": "--inspect-brk=9229",
    },
)
```

**Step 2.** Run the debug target:

```bash
bazel run //path/to:my_test_debug
```

Vitest starts and pauses before executing any test code, waiting for a debugger to attach on port 9229.

**Step 3.** Attach VS Code using the "Attach to Node Process" debug configuration, or use `chrome://inspect` in Chrome.

Source maps are configured automatically: Bazel writes `.js.map` files alongside each `.js` output, so VS Code shows the original `.ts` source with correct line numbers.
