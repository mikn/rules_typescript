# ts_worker_dry_run

Checks that a Cloudflare worker still deploys, without deploying it.

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_worker_dry_run_test")

ts_compile(
    name = "worker",
    srcs = ["src/index.ts"],
    lib = ["esnext", "webworker"],
)

node_modules(
    name = "wrangler_node_modules",
    deps = ["@npm//:wrangler"],
)

ts_worker_dry_run_test(
    name = "deploy_dry_run_test",
    config = "wrangler.jsonc",
    node_modules = ":node_modules",
    deps = [":worker"],
)
```

A dry run does everything a deploy does up to the upload: it resolves the
config, bundles the worker, applies the compatibility date and every binding,
and writes the result to disk. It needs no credentials and no network, so it
runs as a test. A binding removed, a compatibility date that no longer supports
something the worker uses, an entry point that moved — each fails here, before
release.

`ts_worker_dry_run` is the same thing as a `bazel run` target, for when you want
to look at the bundle.

| Rule | Uploads | Belongs in |
|------|---------|------------|
| `ts_worker_dry_run_test` | no | CI. `bazel test //...` runs it on every change |
| `ts_worker_dry_run` | no | a local check, when you want to look at the bundle |
| [`ts_worker_deploy`](ts-worker-deploy.md) | **yes** | a deliberate `bazel run`, by a human or a release job |

## Environments

A config declaring several `[env.*]` sections deploys none of them until it is
told which one, and wrangler warns on every run that it was not. `env_name` is
that choice:

```python
ts_worker_dry_run_test(
    name = "deploy_dry_run_staging_test",
    config = "wrangler.jsonc",
    env_name = "staging",
    node_modules = ":node_modules",
    deps = [":worker"],
)
```

It is an attribute rather than a flag on the command line because the test form
has no command line: `ts_worker_dry_run` takes `--env staging` after
`bazel run`, `ts_worker_dry_run_test` cannot. All three rules share one
attribute set, so [`ts_worker_deploy`](ts-worker-deploy.md) takes it too.

## Config Staging

wrangler writes `.wrangler/tmp` **next to the config file**, not under the
working directory, and a Bazel output directory is read-only. The config and the
worker's compiled `.js` are copied into a scratch directory first, with the
`.js` placed package-relative, which is what lets `"main": "src/index.js"` in a
checked-in config name Bazel's build output. wrangler's state directory has the
same requirement, so `HOME` points into the scratch directory too, and
`WRANGLER_SEND_METRICS=false` removes the only reason a dry run would touch the
network.

## No credentials, enforced

A dry run does not merely avoid authenticating: the launcher takes the means
away. `HOME` and `XDG_CONFIG_HOME` point into the scratch directory, so a
`wrangler login` on disk is invisible, and the `CLOUDFLARE_*` and legacy `CF_*`
variables are **removed** from the environment wrangler is given. A dry run
therefore behaves the same on a logged-in laptop as in CI.

For the same reason, an argument that would undo the dry run — `--no-dry-run`,
`--dry-run=false` — is an error rather than an upload. wrangler parses with
yargs, where every boolean flag has a negated form.

## Publishing

Deploying for real needs credentials, is not reproducible, and must not happen
because something in the build graph changed — so it is a `bazel run` target and
nothing else: [`ts_worker_deploy`](ts-worker-deploy.md).

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `config` | `label` | required | The `wrangler.jsonc`/`.json`/`.toml`. Its `main` is resolved relative to the config, and the worker's `.js` are staged package-relative beside it |
| `deps` | `label_list` | `[]` | The `ts_compile` targets whose `.js` the config's `main` names |
| `env_name` | `string` | `""` | The wrangler environment to deploy, i.e. `--env` |
| `node_modules` | `label` | required | A `node_modules()` target carrying `wrangler`. There is no host fallback |
