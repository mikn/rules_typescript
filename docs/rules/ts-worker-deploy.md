# ts_worker_deploy

Uploads a Cloudflare Worker. **This rule deploys for real.**

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

## The three worker rules

A deploy is a **run target**, the shape Bazel has for a side effect — the same
shape `rules_oci` gives `oci_push`. The side effect happens when a human (or a
release job) runs one target on purpose, and at no other time.

| Rule | Uploads | Belongs in |
|------|---------|------------|
| [`ts_worker_dry_run_test`](ts-worker-dry-run.md) | no | CI. `bazel test //...` runs it on every change |
| [`ts_worker_dry_run`](ts-worker-dry-run.md) | no | a local check, when you want to look at the bundle |
| `ts_worker_deploy` | **yes** | a deliberate `bazel run`, by a human or a release job |

The first two are the same check, and everything a deploy does up to the upload
is verified there. `ts_worker_deploy` adds the upload and nothing else: the
command line it builds is the dry run's, minus `--dry-run`.

## Credentials

wrangler authenticates from the ambient environment, so
`bazel run //path:deploy` needs one of:

- `CLOUDFLARE_API_TOKEN`, plus `CLOUDFLARE_ACCOUNT_ID` when the account is
  ambiguous — the form a release job uses; or
- a `wrangler login` under the real `HOME`.

Those variables are passed straight through to wrangler. The ruleset does not
read, log, or echo any of them, and it stores nothing.

The dry-run rules go the other way: they point `HOME` and `XDG_CONFIG_HOME` at a
scratch directory and **remove** the `CLOUDFLARE_*` and legacy `CF_*` variables
from the environment wrangler gets. A dry run therefore cannot authenticate even
on a machine that is logged in, which is what makes its result the same
everywhere. `ts_worker_deploy` is the only rule here that leaves `HOME` and `CI`
as the caller set them — a deploy may need the login on disk, and whether
wrangler is allowed to prompt is a property of where it is running, not of the
rule.

## What cannot deploy by accident

- `bazel build //...` writes the launcher and its config. It runs nothing.
- `bazel test //...` cannot select a non-test rule.
- `bazel run` takes exactly one target; there is no wildcard form of it.
- The launcher's own default is the dry run. A launcher config that does not say
  `"command": "deploy"` — because it is older, hand-written, or has the value
  misspelt — dry-runs, and an unrecognised value fails the config outright rather
  than being guessed at.
- A `ts_worker_dry_run` target refuses `--no-dry-run` (and `--dry-run=false`) with
  an error. wrangler parses with yargs, where every boolean flag has a negated
  form, so without that guard a run-time argument could undo the dry run.

The rule sets no tags of its own, because none of the above needs one. Add
`tags = ["manual"]` if you would rather the target also stayed out of
`bazel build //...` — this repository does that for its own fixture in
`tests/workers/BUILD.bazel`.

## Run-time arguments

Arguments after `--` are appended after the ones the rule builds, so wrangler's
parser sees them last and they win:

```console
$ bazel run //path:deploy -- --dry-run      # downgrade a deploy; always allowed
$ bazel run //path:deploy -- --var TAG:abc
```

The reverse is refused, as above: nothing on a command line can turn a dry run
into an upload.

## Attributes

Identical to [`ts_worker_dry_run`](ts-worker-dry-run.md#attributes).

| Attribute | Type | Default | Description |
|-----------|------|---------|-------------|
| `config` | `label` | required | The `wrangler.jsonc`/`.json`/`.toml`. Its `main` is resolved relative to the config, and the worker's `.js` are staged package-relative beside it |
| `deps` | `label_list` | `[]` | The `ts_compile` targets whose `.js` the config's `main` names |
| `env_name` | `string` | `""` | The wrangler environment to deploy, i.e. `--env` |
| `node_modules` | `label` | required | A `node_modules()` target carrying `wrangler`. There is no host fallback |
