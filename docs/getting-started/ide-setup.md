# IDE Setup

## Live Resolution (recommended)

rules_typescript ships a **tsserver hook** that resolves modules directly from Bazel's build graph — npm packages, internal packages, and path aliases. No manual `tsconfig.json` maintenance needed.

### Setup

```bash
bazel run //:refresh_tsconfig
```

This generates:
- `.bazel/tsserver-hook.js` — the resolution hook
- `.bazel/tsserver-hook-worker.js` — background worker
- `.bazel/tsserver-launch.json` — editor config snippet
- `tsconfig.json` — minimal compiler options (no paths — the hook handles resolution)

### VS Code

Add to `.vscode/settings.json`:

```json
{
  "typescript.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"
}
```

Restart the TS server: `Cmd+Shift+P` → `TypeScript: Restart TS Server`.

### Neovim (coc-tsserver)

Add to `coc-settings.json`:

```json
{
  "tsserver.tsserver.nodeOptions": "--require .bazel/tsserver-hook.js"
}
```

### Neovim (nvim-lspconfig + typescript-language-server)

```lua
require('lspconfig').ts_ls.setup({
  init_options = {
    tsserver = {
      nodeOptions = "--require .bazel/tsserver-hook.js",
    },
  },
})
```

### Emacs (lsp-mode)

```elisp
(setq lsp-clients-typescript-server-args
  '("--stdio" "--tsserver-path" "tsserver"
    "--tsserver-log-verbosity" "off"
    "--tsserver-nodeOptions" "--require .bazel/tsserver-hook.js"))
```

### Any editor with tsserver

The hook works with any editor that runs tsserver through Node.js. Pass `--require .bazel/tsserver-hook.js` as a Node flag when starting tsserver.

## How It Works

The hook is TypeScript's equivalent of Go's [GOPACKAGESDRIVER](https://jayconrod.com/posts/125/go-editor-support-in-bazel-workspaces):

1. **Worker thread** runs `bazel query` in the background to find all `ts_compile` targets
2. **npm packages** resolved by scanning the `@npm` external repo (already fetched by any previous `bazel` command)
3. **Internal packages** resolved from `bazel-bin` (`.d.ts` after build) or source tree (`.ts` before build)
4. **Path aliases** read from `# gazelle:ts_path_alias` directives in BUILD files
5. **File watcher** monitors BUILD files, `pnpm-lock.yaml`, and `bazel-bin` — re-resolves automatically when they change

The main thread is never blocked — the worker runs `bazel query` asynchronously and posts results back. tsserver returns "unresolved" briefly on first load, then resolves once the worker completes (~1-2 seconds).

### Resolution priority

1. `.d.ts` in `bazel-bin` — fast, precise (available after `bazel build`)
2. `.ts` source file — always available, slower for tsserver to process
3. npm package types from external repo — always available after first `bazel` command

### No build required

Basic resolution works without `bazel build`. The source `.ts` files are always on disk, and npm packages are fetched by the repository rule (triggered by any `bazel` command, including `bazel run //:gazelle`). Running `bazel build` improves resolution by providing `.d.ts` files, but is not required for the IDE to work.

## Debugging

Set `TSSERVER_HOOK_DEBUG=1` in your environment to see resolution decisions in the tsserver log.

## Debugging Tests in VS Code

To attach a debugger to vitest running inside the Bazel sandbox:

```python
ts_test(
    name = "my_test_debug",
    srcs = ["my.test.ts"],
    deps = [":my_lib"],
    tags = ["manual"],
    env = {"NODE_OPTIONS": "--inspect-brk=9229"},
)
```

```bash
bazel run //path/to:my_test_debug
```

Vitest pauses before executing, waiting for a debugger on port 9229. Attach VS Code via "Attach to Node Process" or use `chrome://inspect`. Source maps are configured automatically.
