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
    node_modules = ":wrangler_node_modules",
    deps = [":worker"],
)
```

A dry run does everything a deploy does up to the upload: it resolves the
config, bundles the worker, applies the compatibility date and every binding,
and writes the result to disk. It needs no credentials and no network, so it
runs as a test. A removed binding, a compatibility date that no longer supports
something the worker uses, or a moved entry point fails here.

`ts_worker_dry_run` is the same check as a `bazel run` target, for inspecting
the bundle.

| Rule | Uploads | Runs in |
|------|---------|---------|
| `ts_worker_dry_run_test` | no | CI; `bazel test //...` runs it on every change |
| `ts_worker_dry_run` | no | a local `bazel run`, to inspect the bundle |
| [`ts_worker_deploy`](ts-worker-deploy.md) | yes | a deliberate `bazel run`, by a human or a release job |

## Environments

A config declaring several `[env.*]` sections deploys none of them until told
which; wrangler warns on every run that it was not told. `env_name` names the
environment:

```python
ts_worker_dry_run_test(
    name = "deploy_dry_run_staging_test",
    config = "wrangler.jsonc",
    env_name = "staging",
    node_modules = ":wrangler_node_modules",
    deps = [":worker"],
)
```

`ts_worker_dry_run` also takes `--env staging` after `bazel run`;
`ts_worker_dry_run_test` has no command line. All three rules share one
attribute set, so [`ts_worker_deploy`](ts-worker-deploy.md) takes `env_name`
too.

## Config Staging

wrangler writes `.wrangler/tmp` next to the config file, not under the working
directory, and a Bazel output directory is read-only. The config and the
worker's compiled `.js` are copied into a scratch directory first, with the
`.js` placed package-relative, so `"main": "src/index.js"` in a checked-in
config names Bazel's build output. `HOME` points into the scratch directory too,
for wrangler's state directory, and `WRANGLER_SEND_METRICS=false` keeps a dry
run off the network.

## Credential Removal

The launcher points `HOME` and `XDG_CONFIG_HOME` into the scratch directory, so
a `wrangler login` on disk is invisible, and removes the `CLOUDFLARE_*` and
legacy `CF_*` variables from the environment wrangler is given. A dry run
behaves the same on a logged-in laptop as in CI.

`--no-dry-run` and `--dry-run=false` are errors. wrangler parses with yargs,
where every boolean flag has a negated form.

## Publishing

Deploying needs credentials and is not reproducible, so it is a `bazel run`
target: [`ts_worker_deploy`](ts-worker-deploy.md).

## Attributes

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `config` | `label` | required | The `wrangler.jsonc`/`.json`/`.toml`. Its `main` is resolved relative to the config, and the worker's `.js` are staged package-relative beside it |
| `deps` | `label_list` | `[]` | The `ts_compile` targets whose `.js` the config's `main` names |
| `env_name` | `string` | `""` | The wrangler environment to deploy, i.e. `--env` |
| `node_modules` | `label` | required | A `node_modules()` target carrying `wrangler`. There is no host fallback |
