# Benchmark

The same work done two ways on one checkout of the Lovable monorepo at parity,
measured twice: at commit f9fd041 of the trial with this ruleset at f123f53,
and at commit 613f2b2 with this ruleset at 5049c1b (TypeScript 7.0.2, node
24.14.1, pnpm 11.5.3, Bazel 9.2.0 both times). The checkout's own commands as
CI and a developer run them, and `bazel build //...` / `bazel test //...` with
`--@rules_typescript//ts:declarations=tsgo`, `--norun_validations` and a disk
cache. Four cache states, three runs per cell, the median with its spread;
`tools/bench_parity.sh` ran both (`OTHER_CORES=2 REDO=6
SYSTEM_PROCS=falcon-sensor-bpf`, the excluded targets below). The runner
passes `--@rules_typescript//ts:lint=@rules_typescript//ts:no_lint`, which
turns the linter off alone; `--norun_validations` would skip `TsgoCheck` too.
A target red at the parity proof is left out of the test-everything cells on
both sides, together with the checkout rows that run the same files: Bazel
never caches a failed test, so a red target would put its own run into every
warm row. At
f9fd041 thirteen `ts_test` targets were left out -- the nine red at that
proof and the four that share a CI row with one of them: `//web:web_test`,
`//web:node_tooling_test`, `//web:scripts_lib_test`, `//packages/ui:ui_test`,
`//packages/applocal-mcp:applocal-mcp_test`,
`//packages/canvas-sdk:canvas-sdk_test` (test.yml:1105, one vitest run over
those members, and its setup step test.yml:1093 with
`//web:paraglide_messages`), `//desktop:desktop_test` (test.yml:1378),
`//firebase/functions:functions_test` (test-firebase-functions.yml:24),
`//workers/mcp-server/test:test_test` (cf-workers-test.yml in that directory),
`//npm-packages/lovable-auth-js:lovable-auth-js_test`,
`//npm-packages/lovite:lovite_test`,
`//packages/figma-plugin:figma-plugin_test` and
`//workers/rudderstack-proxy/test:test_test` (no CI row). At 613f2b2 ten:
the same six of test.yml:1105 and test.yml:1093 (`web_test` red on two
5000 ms cases, `ui_test` on one chart re-bless and one 5000 ms case, the
other four green and in that row), `//desktop:desktop_test`,
`//workers/mcp-server/test:test_test`, `//npm-packages/lovite:lovite_test`
and `//workers/rudderstack-proxy/test:test_test`; every red at that proof is
an owner's row or a load row, none the ruleset's, and functions_test,
lovable-auth-js, figma-plugin and lovable-vite-tanstack are in. Nothing was
red at that proof's build (`bazel build //...` exit 0 over 426 targets), so
every `//...` cell holds the whole tree. The test lane builds the excluded
targets once, after its warm row (106.7 s at f9fd041, 24.6 s at 613f2b2), so
the edit rows start from a built tree as the checkout's do. The edit rows'
patterns stay whole: `//web/...` holds `web_test`.

The machine: 22 cores, 62 GB, btrfs on dm-crypt; 54-70 GB of swap
in use throughout the first run, 7-20 GB throughout the second. A
cell starts when processes outside the runner's tree use under 2 cores over
3 s, and a cell during which they averaged 2 or more is redone with its
segment; the kernel's threads and the security sensor (`falcon-sensor-bpf`,
whose CPU follows the benchmark's own exec and file activity) are neither
side. Every kept cell's other work is in the table. At f9fd041 one cell of
the 51 was redone (run 3's `vitest run --changed`, 5.97 cores of other work).
At 613f2b2 three cells of the 51 were redone with their segments (run 1's
`tsc -p web` at 2.98 cores, run 2's `bazel build //web/...` at 3.50 and
run 2's cached `bazel build //...` at 2.02). The gate waited up to 21 s for
other work to fall under 2 cores.

## The table

Seconds, the median of three runs with the minimum and maximum; other work in
cores, the median run's. An exit code stands where it is not 0: the failing
step's for the checkout, Bazel's for Bazel. The first pair of columns is the
run at f9fd041/f123f53, the second the run at 613f2b2/5049c1b.

| Work | Cache state | Checkout, f9fd041 | Bazel, f123f53 | Checkout, 613f2b2 | Bazel, 5049c1b | Other work at f9fd041 (checkout / Bazel) | Other work at 613f2b2 (checkout / Bazel) |
|---|---|---|---|---|---|---|---|
| Typecheck everything | cold | 31.2 (26.9-31.5) | 812.7 (791.6-887.3) | 23.5 (23.0-23.6) | 356.7 (351.7-357.2) | 0.75 / 0.78 | 0.43 / 1.63 |
| Typecheck everything | warm, nothing changed | 26.7 (25.7-31.9) | 58.9 (48.6-73.8) | 23.0 (22.7-24.6) | 3.5 (3.4-3.9) | 1.00 / 0.96 | 0.65 / 0.26 |
| Typecheck everything | fresh output base, populated disk cache |  | 400.7 (376.6-421.4) |  | 68.6 (65.7-70.1) | 0.45 | 0.08 |
| Test everything | cold | 131.9 (129.5-133.4) | 799.8 (761.7-833.6) | 115.2 (115.0-115.5), exit 1/1 | 422.7 (419.4-442.6) | 0.57 / 1.27 | 0.38 / 0.27 |
| Test everything | warm, nothing changed | 125.2 (125.1-126.3) | 37.2 (35.2-43.7) | 111.5 (111.3-111.5), exit 1/1 | 3.6 (3.3-3.7) | 0.53 / 0.67 | 0.37 / 0.16 |
| Test everything | fresh output base, populated disk cache |  | 321.0 (297.7-338.0) |  | 66.3 (66.1-70.2) | 0.36 | 0.13 |
| One-line change in web, re-check | warm | 19.5 (18.4-20.0) | 184.5 (146.1-186.2) | 13.2 (12.5-13.6) | 144.9 (133.5-145.3) | 1.70 / 1.07 | 0.55 / 1.41 |
| One-line change in web, re-test the affected suites | warm | 48.1 (47.4-64.3), exit 134 | 744.3 (743.9-752.9), exit 3 | 49.3 (43.3-55.5), exit 134 | 610.7 (608.7-639.9), exit 3 | 0.54 / 0.75 | 0.29 / 0.42 |
| One-line change in a leaf worker, re-test | warm | 1.7 (1.5-2.1) | 9.2 (8.0-9.7) | 1.1 (1.1-1.1) | 5.5 (5.4-5.5) | 0.48 / 0.57 | 0.28 / 0.36 |

The checkout's way: `.github/scripts/typecheck.sh`; the CI `run:` lines that
run TypeScript tests outside the excluded rows (11 at f9fd041,
12 at 613f2b2) plus cf-workers-test.yml's step in the 21 worker
directories CI tests, one after another; `tsc -p web --noEmit`;
`pnpm --filter=web run test:run --changed`; cf-workers-test.yml's step in
workers/download. Bazel's: `bazel build //...`; `bazel test //...` minus the
excluded labels; `bazel build //web/...`; `bazel test //web/...`;
`bazel test //workers/download/...`. The edit is `;` appended to
web/shared/lib/markdown/markedRenderer.ts and to workers/download/src/index.ts.

## What each difference is

Every measurement below is from the median run's log of its cell at
613f2b2/5049c1b -- the run whose wall is the table's median -- unless it
names another run; a figure "at f123f53" is from the median run's log of
the same cell in the first run; a range in parentheses is the table's
minimum and maximum.

**Typecheck everything, cold.** typecheck.sh builds `@lovablelabs/agent-sdk`,
compiles the paraglide messages and runs `tsc --noEmit --incremental false`
over the 22 tsconfig.json files in its list at 613f2b2, one process each, in
sequence: 23.5 s. Bazel runs 16156 actions (12828 internal, 3328 in
sandboxes) with a critical path of 277.8 s. The internal actions are the
importers' `node_modules` links: a target's npm packages are symlinks into
pnpm's virtual store, one `NpmStore` copy per package version shared by
every target that resolves it, where at f123f53 every target copied its own
tree from the pnpm store (5332 actions, 1095 in sandboxes, seven tree copies
of 128-194 s each, 812.7 s). The long action is `TsgoDeclare //web:web`,
110 s of that path (213 s at f123f53): under `--declarations=tsgo` the
check of a program is its declaration emit, tsgo over the store trees the
target's links reach with `--explainFiles` (ts/private/actions/tsgo.bzl
checks that listing against the ownership manifest), so web is checked and
its declarations written where typecheck.sh only checks. The cold build also
compiles the `oxc-bazel` tool from Rust source and builds the Go toolchain
it fetched (`GoToolchainBinaryBuild`, 22 s), once per output base.

**Typecheck everything, warm.** typecheck.sh keeps no state (`--incremental
false`) and repeats the cold row: 23.0 s. Bazel runs `1 process: 1
internal` with a critical path of 0.02 s: 3.5 s, the analysis of 426
targets. At f123f53 the same cell re-checked 327 actions against the trees'
millions of files and took 58.9 s.

**Typecheck everything, a fresh output base over a populated disk cache.**
The shape of a CI runner with a shared cache. `16156 processes: 3328 disk
cache hit, 12828 internal`: every action the cold cell ran in a sandbox is
a cache hit -- the store copies, the compiles, the declares, the Go and
Rust tools -- and none executes; the 68.6 s are the analysis, the fetches
and the links, critical path 14.4 s. At f123f53 the 140 tree copies carried
`no-cache` and executed again in every fresh output base: 400.7 s. The
checkout has no counterpart: its caches are the vitest cache and
typecheck.sh's own outputs.

**Test everything, cold.** The checkout runs 33 rows -- 31 of them vitest
4.1.5; 292 files and 6099 tests pass -- in sequence, each with its own node
and pnpm start: 115.2 s. Two rows exit 1 in every
run, workers/o11y-tail-worker and workers/api-gateway: every test file of
theirs fails at collection with `TypeError: Cannot read properties of
undefined (reading 'config')`. Since b05ba7e declared
`@vitest/coverage-istanbul` in these two manifests -- neither declares
`vitest` -- pnpm links the peer's `vitest` bin into the member's
`node_modules/.bin`, one instance, while the member's files resolve
`vitest` from the root's `node_modules`, another; at f9fd041 the member had
no `.bin/vitest` and the root's ran both sides. Their Bazel targets pass:
the store gives a target one `vitest`. Bazel runs 15907 actions (12574
internal, 3379 in sandboxes) for 46 test targets; the tests' own times sum
to 201.6 s (proxy-worker2's 55.5 s the longest) and the critical path is
348.7 s: the store copies, the compiles and declares of everything the
tests need come first, `TsgoDeclare //web:web` 138 s on this lane. The
checkout's rows are what CI runs; the 46 targets are every `ts_test`
outside the exclusions, CI row or not.

**Test everything, warm.** vitest keeps no result cache and runs the rows
again: 111.5 s, the same two rows exit 1. Bazel runs none: `Executed 0 out
of 46 tests`, `1 process: 46 action cache hit, 1 internal`, 3.6 s (37.2 s
at f123f53).

**Test everything, a fresh output base over a populated disk cache.**
`15907 processes: 3379 disk cache hit, 12574 internal`, no test run: 66.3 s,
critical path 12.3 s. At f123f53 the 127 tree copies the cache never held
made it 321.0 s.

**One-line change in web, re-check.** `tsc -p web --noEmit` checks the whole
web program with no state: 13.2 s. `bazel build //web/...` re-runs the
three sandboxed actions on `//web:web` whose inputs changed -- `TsConfig`,
the `TsEmit` compile and `TsgoDeclare` at 135 s -- and hits the action cache
for the other 13: the declarations a `;` produces are byte-identical, so
nothing downstream re-runs; 144.9 s (133.5-145.3; 184.5 s at f123f53). The
target is the unit: one line in web re-checks and re-emits web, and the emit
is the cost tsc's `--noEmit` never pays.

**One-line change in web, re-test the affected suites.** The checkout's way
does not finish: `vitest run --changed` computes the affected set over web's
test files and dies at V8's heap limit (`FATAL ERROR: Reached heap limit`,
SIGABRT, exit 134) in every run, after 49.3 s (43.3-55.5); nothing in the
checkout raises the heap. `bazel test //web/...` runs the three test targets
whose inputs changed: `web_test` in 474.2 s (red, one test of three: its two
cases at their 5000 ms budget, goblinStore and system-status.server, the
load rows the exclusion names), `node_tooling_test` 9.6 s,
`scripts_lib_test` 0.5 s, after the same re-emit of web on the test lane
(`TsgoDeclare //web:web` 127 s); 610.7 s (744.3 s at f123f53). A target is
the unit of re-testing, so a one-line change in web runs web's whole suite.

**One-line change in a leaf worker, re-test.** The checkout runs vitest in
workers/download: one file, 76 tests, 196 ms, 1.1 s with pnpm's start; CI
typechecks no worker (lint-js.yml generates `worker-configuration.d.ts` for
type-aware oxlint alone, and typecheck.sh's list of 22 tsconfigs at 613f2b2
holds none). Bazel runs six sandboxed actions -- among them `TsCodegen` for
the worker's types and the test itself at 0.5 s -- and hits the action cache
for two: 5.5 s (9.2 s at f123f53), of which the type check is work the
checkout's CI never does.

**What a build costs the machine besides itself.** During the cold Bazel
check (356.7 s) the runner's own processes used 2576.9 CPU-seconds, the
kernel's threads 241.8 and the security sensor 406.9; during typecheck.sh
(23.5 s) the kernel 3.3 and the sensor 8.5. The threads copy and encrypt
what Bazel writes; the sensor inspects what it executes and opens. At
f123f53 the same cell cost 5751.8, 1108.9 and 1401.2: the trees copied per
target were most of what the threads and the sensor saw.
