# ts_worker_deploy

Uploads a Cloudflare Worker. `bazel run` on this target deploys.

```python
load("@rules_typescript//npm:defs.bzl", "node_modules")
load("@rules_typescript//ts:defs.bzl", "ts_compile", "ts_worker_deploy")

ts_compile(
    name = "worker",
    srcs = ["src/index.ts"],
    lib = ["esnext", "webworker"],
)

node_modules(
    name = "wrangler_node_modules",
    deps = ["@npm//:wrangler"],
)

ts_worker_deploy(
    name = "deploy",
    config = "wrangler.jsonc",
    node_modules = ":wrangler_node_modules",
    deps = [":worker"],
)
```

```console
$ bazel run //path/to:deploy
```

## The Three Worker Rules

A deploy is a run target, the same shape `rules_oci` gives `oci_push`. The
upload happens when someone runs the target, and at no other time.

| Rule | Uploads | Runs in |
|------|---------|---------|
| [`ts_worker_dry_run_test`](ts-worker-dry-run.md) | no | CI; `bazel test //...` runs it on every change |
| [`ts_worker_dry_run`](ts-worker-dry-run.md) | no | a local `bazel run`, to inspect the bundle |
| `ts_worker_deploy` | yes | a deliberate `bazel run`, by a human or a release job |

The first two are the same check. `ts_worker_deploy`'s command line is the dry
run's minus `--dry-run`.

## Credentials

wrangler authenticates from the ambient environment, so
`bazel run //path:deploy` needs one of:

- `CLOUDFLARE_API_TOKEN`, plus `CLOUDFLARE_ACCOUNT_ID` when the account is
  ambiguous (the form a release job uses); or
- a `wrangler login` under the real `HOME`.

Those variables are passed straight through to wrangler. The ruleset does not
read, log, or echo any of them, and it stores nothing.

The dry-run rules point `HOME` and `XDG_CONFIG_HOME` at a scratch directory and
remove the `CLOUDFLARE_*` and legacy `CF_*` variables from the environment
wrangler gets, so a dry run cannot authenticate on a logged-in machine.
`ts_worker_deploy` is the only worker rule that leaves `HOME` and `CI` as the
caller set them.

## Commands That Do Not Deploy

- `bazel build //...` writes the launcher and its config. It runs nothing.
- `bazel test //...` cannot select a non-test rule.
- `bazel run` takes exactly one target and has no wildcard form.
- The launcher's default is the dry run. A launcher config that does not say
  `"command": "deploy"` dry-runs; an unrecognised value fails the config.
- A `ts_worker_dry_run` target refuses `--no-dry-run` and `--dry-run=false` with
  an error. wrangler parses with yargs, where every boolean flag has a negated
  form.

The rule sets no tags. `tags = ["manual"]` keeps the target out of
`bazel build //...`; this repository sets it on its own fixture in
`tests/workers/BUILD.bazel`.

## Run-Time Arguments

Arguments after `--` are appended after the ones the rule builds, so wrangler's
parser sees them last and they win:

```console
$ bazel run //path:deploy -- --dry-run      # downgrade a deploy; always allowed
$ bazel run //path:deploy -- --var TAG:abc
```

The reverse is refused: no command-line argument turns a dry run into an
upload.

## Attributes

Identical to [`ts_worker_dry_run`](ts-worker-dry-run.md#attributes).

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `config` | `label` | required | The `wrangler.jsonc`/`.json`/`.toml`. Its `main` is resolved relative to the config, and the worker's `.js` are staged package-relative beside it |
| `deps` | `label_list` | `[]` | The `ts_compile` targets whose `.js` the config's `main` names |
| `env_name` | `string` | `""` | The wrangler environment to deploy, i.e. `--env` |
| `node_modules` | `label` | required | A `node_modules()` target carrying `wrangler`. There is no host fallback |
