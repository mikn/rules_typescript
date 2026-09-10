# Benchmark

The same work done two ways on one checkout of the Lovable monorepo at parity
(commit f9fd041 of the trial, this ruleset at f123f53, TypeScript 7.0.2, node
24.14.1, pnpm 11.5.3, Bazel 9.2.0): the checkout's own commands as CI and a
developer run them, and `bazel build //...` / `bazel test //...` with
`--@rules_typescript//ts:declarations=tsgo`, `--norun_validations` and a disk
cache. Four cache states, three runs per cell, the median with its spread;
`tools/bench_parity.sh` ran it (`OTHER_CORES=2 REDO=6
SYSTEM_PROCS=falcon-sensor-bpf`, the excluded targets below). Thirteen
`ts_test` targets -- the nine red at the parity proof and the four that share
a CI row with one of them -- are left out of the test-everything cells on both
sides, together with the checkout rows that run the same files:
`//web:web_test`, `//web:node_tooling_test`, `//web:scripts_lib_test`,
`//packages/ui:ui_test`, `//packages/applocal-mcp:applocal-mcp_test`,
`//packages/canvas-sdk:canvas-sdk_test` (test.yml:1105, one vitest run over
those members, and its setup step test.yml:1093 with
`//web:paraglide_messages`), `//desktop:desktop_test` (test.yml:1378),
`//firebase/functions:functions_test` (test-firebase-functions.yml:24),
`//workers/mcp-server/test:test_test` (cf-workers-test.yml in that directory),
and four targets no CI row runs:
`//npm-packages/lovable-auth-js:lovable-auth-js_test`,
`//npm-packages/lovite:lovite_test`,
`//packages/figma-plugin:figma-plugin_test`,
`//workers/rudderstack-proxy/test:test_test`. Bazel never caches a failed test,
so a red target would put its own run into every warm row. The test lane
builds the excluded targets once, after its warm row (106.7 s, 105.5-111.7),
so the edit rows start from a built tree as the checkout's do. The edit rows'
patterns stay whole: `//web/...` holds `web_test`.

The machine: 22 cores, 62 GB, btrfs on dm-crypt, 54-70 GB of swap in use
throughout. A cell starts when processes outside the runner's tree use under 2
cores over 3 s, and a cell during which they averaged 2 or more is redone with
its segment; the kernel's threads and the security sensor (`falcon-sensor-bpf`,
whose CPU follows the benchmark's own exec and file activity) are neither side.
Every kept cell's other work is in the table. One cell of the 51 was redone
(run 3's `vitest run --changed`, 5.97 cores of other work; its segment ran
again after both lanes were returned to the pre-edit tree and the attempt's
cache entries removed).

## The table

Seconds, the median of three runs with the minimum and maximum; other work in
cores, the median run's. An exit code stands where it is not 0: the failing
step's for the checkout, Bazel's for Bazel.

| Work | Cache state | The checkout's way | Bazel | Other work (checkout / Bazel) |
|---|---|---|---|---|
| Typecheck everything | cold | 31.2 (26.9-31.5) | 812.7 (791.6-887.3) | 0.75 / 0.78 |
| Typecheck everything | warm, nothing changed | 26.7 (25.7-31.9) | 58.9 (48.6-73.8) | 1.00 / 0.96 |
| Typecheck everything | fresh output base, populated disk cache | | 400.7 (376.6-421.4) | 0.45 |
| Test everything | cold | 131.9 (129.5-133.4) | 799.8 (761.7-833.6) | 0.57 / 1.27 |
| Test everything | warm, nothing changed | 125.2 (125.1-126.3) | 37.2 (35.2-43.7) | 0.53 / 0.67 |
| Test everything | fresh output base, populated disk cache | | 321.0 (297.7-338.0) | 0.36 |
| One-line change in web, re-check | warm | 19.5 (18.4-20.0) | 184.5 (146.1-186.2) | 1.70 / 1.07 |
| One-line change in web, re-test the affected suites | warm | 48.1 (47.4-64.3), exit 134 | 744.3 (743.9-752.9), exit 3 | 0.54 / 0.75 |
| One-line change in a leaf worker, re-test | warm | 1.7 (1.5-2.1) | 9.2 (8.0-9.7) | 0.48 / 0.57 |

The checkout's way: `.github/scripts/typecheck.sh`; the 11 CI `run:` lines
that run TypeScript tests outside the excluded rows plus cf-workers-test.yml's
step in the 21 worker directories CI tests, one after another; `tsc -p web
--noEmit`; `pnpm --filter=web run test:run --changed`; cf-workers-test.yml's
step in workers/download. Bazel's: `bazel build //...`; `bazel test //...`
minus the excluded labels; `bazel build //web/...`; `bazel test //web/...`;
`bazel test //workers/download/...`. The edit is `;` appended to
web/shared/lib/markdown/markedRenderer.ts and to workers/download/src/index.ts.

## What each difference is

Every measurement below is from the median run's log of its cell -- the run
whose wall is the table's median; a range in parentheses is the table's
minimum and maximum.

**Typecheck everything, cold.** typecheck.sh builds `@lovablelabs/agent-sdk`,
compiles the paraglide messages and runs `tsc --noEmit --incremental false`
over the 22 tsconfig.json files in its list at f9fd041, one process each, in
sequence: 31 s. Bazel runs 5332 actions (4237 internal, 1095 in sandboxes)
with a critical path of 677 s. The long ones are the `NodeModulesTree`
actions f9fd041 ran -- there every target staged its own node_modules tree,
copied from the pnpm store; seven of them 128-194 s -- and `TsgoDeclare
//web:web`, 213 s:
under `--declarations=tsgo` the check of a program is its declaration emit,
tsgo over the staged tree with `--explainFiles` (ts/private/actions/tsgo.bzl
checks that listing against the ownership manifest), so web is checked and
its declarations written where typecheck.sh only checks. The cold build also
compiles the `oxc-bazel` tool from Rust source and builds the Go toolchain it
fetched, once per output base.

**Typecheck everything, warm.** typecheck.sh keeps no state (`--incremental
false`) and repeats the cold row: 26.7 s. Bazel executes nothing: `327 action
cache hit, 1 internal`, and the 58.9 s are that one internal step's 38.6 s
critical path plus analysis -- the check that the previous build's outputs,
f9fd041's node_modules trees with their millions of files among them, are
what it wrote.

**Typecheck everything, a fresh output base over a populated disk cache.**
The shape of a CI runner with a shared cache. 955 of the 1095 sandboxed
actions are disk cache hits; the other 140 execute: the `NodeModulesTree`
actions carried `no-cache` at f9fd041 (`NEVER_FROM_A_CACHE` in that commit's
ts/private/node_modules.bzl: a cache fetch did not reliably reproduce every
path of a tree, so the tree was copied from files already on disk every
time), and they are the 400.7 s. The
checkout has no counterpart: its caches are the vitest cache and typecheck.sh's
own outputs.

**Test everything, cold.** The checkout runs 32 rows -- 31 of them vitest
4.1.5 over 297 files and 6179 tests -- in sequence, each with its own node and
pnpm start: 131.9 s. Bazel runs 5153 actions (4081 internal, 1115 in sandboxes)
for 43 test targets; the tests' own times sum to 230 s (proxy-worker2's 78.7 s
the longest) and the critical path is 692 s: f9fd041's node_modules trees,
the compiles and declares of everything the tests need come first, as in the
check lane.
The checkout's 32 rows are what CI runs; the 43 targets are every `ts_test`
outside the exclusions, CI row or not.

**Test everything, warm.** vitest keeps no result cache and runs the 6179
tests again: 125.2 s. Bazel runs none: `Executed 0 out of 43 tests`, one
internal action, 37.2 s.

**Test everything, a fresh output base over a populated disk cache.** 988
disk cache hits and the 127 `NodeModulesTree` executions f9fd041's cache
never held: 321.0 s, with no test run.

**One-line change in web, re-check.** `tsc -p web --noEmit` checks the whole
web program with no state: 19.5 s. `bazel build //web/...` re-runs the three
sandboxed actions on `//web:web` whose inputs changed -- `TsConfig`, the
`TsEmit` compile and `TsgoDeclare` at 147 s -- and hits the action cache
for the other 15: the declarations a `;` produces are byte-identical, so
nothing downstream re-runs. The target is the unit: one line in web re-checks
and re-emits web, and the emit is the cost tsc's `--noEmit` never pays.

**One-line change in web, re-test the affected suites.** The checkout's way
does not finish: `vitest run --changed` computes the affected set over web's
test files and dies at V8's heap limit (`FATAL ERROR: Reached heap limit`, the
heap at 4085 MB after its last GC, SIGABRT, exit 134) in every run, after
48.1 s (47.4-64.3); nothing in the checkout raises the heap. `bazel test
//web/...` runs the three test targets whose inputs changed: `web_test` over
its 2235 files in 577.7 s (red: the parity proof's RULESET cases),
`node_tooling_test` 13.6 s, `scripts_lib_test` 1.9 s, after the same re-emit
of web on the test lane; 744.3 s. A target is the unit of re-testing, so a
one-line change in web runs web's whole suite.

**One-line change in a leaf worker, re-test.** The checkout runs vitest in
workers/download: one file, 76 tests, 268 ms, 1.7 s with pnpm's start; CI
typechecks no worker (lint-js.yml generates `worker-configuration.d.ts` for
type-aware oxlint alone, and typecheck.sh's list of 22 tsconfigs at f9fd041
holds none). Bazel runs six sandboxed actions -- among them `TsCodegen` for
the worker's types and the test itself at 0.9 s -- and hits the action cache
for three: 9.2 s, of which the type check is work the checkout's CI never does.

**What a build costs the machine besides itself.** During the cold Bazel check
(812.7 s) the runner's own processes used 5751.8 CPU-seconds, the kernel's
threads 1108.9 and the security sensor 1401.2; during typecheck.sh (31.2 s)
the kernel 15.2 and the sensor 9.1. The threads copy and encrypt what Bazel
writes; the sensor inspects what it executes and opens. Both scaled with
f9fd041's node_modules trees.
