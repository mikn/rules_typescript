# Benchmark

The same work done two ways on one checkout of the Lovable monorepo at parity,
measured twice: at commit f9fd041 of the trial with this ruleset at f123f53,
and at commit 83a141e with this ruleset at e864cab (TypeScript 7.0.2, node
24.14.1, pnpm 11.5.3, Bazel 9.2.0 both times). The checkout's own commands as
CI and a developer run them, and `bazel build //...` / `bazel test //...` with
`--@rules_typescript//ts:declarations=tsgo`, `--norun_validations` and a disk
cache. Four cache states, three runs per cell, the median with its spread;
`tools/bench_parity.sh` ran both (`OTHER_CORES=2 REDO=6
SYSTEM_PROCS=falcon-sensor-bpf`, the excluded targets below). A target red at
the parity proof is left out of the test-everything cells on both sides,
together with the checkout rows that run the same files: Bazel never caches a
failed test, so a red target would put its own run into every warm row. At
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
`//workers/rudderstack-proxy/test:test_test` (no CI row). At 83a141e eleven:
the same six of test.yml:1105 and test.yml:1093, `//desktop:desktop_test`,
`//workers/mcp-server/test:test_test`,
`//npm-packages/lovable-vite-tanstack:lovable-vite-tanstack_test`
(validate-packages.yml:184), `//npm-packages/lovite:lovite_test` and
`//workers/rudderstack-proxy/test:test_test`; lovable-auth-js and figma-plugin
were green at that proof and are in. One target was red at that proof's
build, `//firebase/functions:functions` (TS2688: tsaction's `types` writer
under the program's own `typeRoots`), and a failed action ends a `bazel
build` or `bazel test` where it stands, so it leaves every `//...` cell on
both lanes with the two targets behind it, `//firebase/functions:functions_test`
and its store member (`EXCLUDE_BUILD`; typecheck.sh checks no firebase
tsconfig, so the checkout's typecheck row loses nothing). The test lane builds
the excluded targets once, after its warm row (106.7 s at f9fd041, 34.5 s at
83a141e), so the edit rows start from a built tree as the checkout's do. The
edit rows' patterns stay whole: `//web/...` holds `web_test`.

The machine: 22 cores, 62 GB, btrfs on dm-crypt; 54-70 GB of swap
in use throughout the first run, 13-28 GB throughout the second. A
cell starts when processes outside the runner's tree use under 2 cores over
3 s, and a cell during which they averaged 2 or more is redone with its
segment; the kernel's threads and the security sensor (`falcon-sensor-bpf`,
whose CPU follows the benchmark's own exec and file activity) are neither
side. Every kept cell's other work is in the table. At f9fd041 one cell of
the 51 was redone (run 3's `vitest run --changed`, 5.97 cores of other work).
At 83a141e four cells of the 51 were redone with their
segments (run 1's `tsc -p web` at 2.54 cores and its `bazel build //web/...`
at 2.56, run 2's warm typecheck.sh at 2.70 and its cold `bazel build //...`
at 2.24), and run 2's first check lane spanned a 36-minute suspend of the
machine (the lid closed) inside its cold Bazel cell, wall 2518.6 s, and was
redone with its segment when the next cell was contended: the runner does
not detect a suspend, the run's log records it. The gate waited up to 130 s
for other work to fall under 2 cores.

## The table

Seconds, the median of three runs with the minimum and maximum; other work in
cores, the median run's. An exit code stands where it is not 0: the failing
step's for the checkout, Bazel's for Bazel. The first pair of columns is the
run at f9fd041/f123f53, the second the run at 83a141e/e864cab.

| Work | Cache state | Checkout, f9fd041 | Bazel, f123f53 | Checkout, 83a141e | Bazel, e864cab | Other work at f9fd041 (checkout / Bazel) | Other work at 83a141e (checkout / Bazel) |
|---|---|---|---|---|---|---|---|
| Typecheck everything | cold | 31.2 (26.9-31.5) | 812.7 (791.6-887.3) | 30.0 (26.2-30.6) | 392.0 (348.1-443.6) | 0.75 / 0.78 | 0.45 / 1.28 |
| Typecheck everything | warm, nothing changed | 26.7 (25.7-31.9) | 58.9 (48.6-73.8) | 25.9 (22.1-39.9) | 3.7 (3.5-5.9) | 1.00 / 0.96 | 1.67 / 1.63 |
| Typecheck everything | fresh output base, populated disk cache |  | 400.7 (376.6-421.4) |  | 72.5 (65.1-104.7) | 0.45 | 0.48 |
| Test everything | cold | 131.9 (129.5-133.4) | 799.8 (761.7-833.6) | 125.0 (124.1-237.0), exit 1/1 | 474.2 (458.1-655.8) | 0.57 / 1.27 | 0.69 / 1.18 |
| Test everything | warm, nothing changed | 125.2 (125.1-126.3) | 37.2 (35.2-43.7) | 126.1 (115.8-234.9), exit 1/1 | 10.3 (3.5-119.1) | 0.53 / 0.67 | 1.17 / 0.89 |
| Test everything | fresh output base, populated disk cache |  | 321.0 (297.7-338.0) |  | 72.4 (71.1-210.2) | 0.36 | 0.31 |
| One-line change in web, re-check | warm | 19.5 (18.4-20.0) | 184.5 (146.1-186.2) | 15.6 (13.2-22.7) | 190.9 (139.5-244.5) | 1.70 / 1.07 | 1.70 / 1.27 |
| One-line change in web, re-test the affected suites | warm | 48.1 (47.4-64.3), exit 134 | 744.3 (743.9-752.9), exit 3 | 62.4 (46.0-91.9), exit 134 | 725.0 (638.7-801.0), exit 3 | 0.54 / 0.75 | 0.52 / 1.80 |
| One-line change in a leaf worker, re-test | warm | 1.7 (1.5-2.1) | 9.2 (8.0-9.7) | 1.5 (1.1-2.7) | 7.8 (5.8-10.9) | 0.48 / 0.57 | 0.38 / 0.32 |

The checkout's way: `.github/scripts/typecheck.sh`; the CI `run:` lines that
run TypeScript tests outside the excluded rows (11 at f9fd041,
10 at 83a141e) plus cf-workers-test.yml's step in the 21 worker
directories CI tests, one after another; `tsc -p web --noEmit`;
`pnpm --filter=web run test:run --changed`; cf-workers-test.yml's step in
workers/download. Bazel's: `bazel build //...`; `bazel test //...` minus the
excluded labels; `bazel build //web/...`; `bazel test //web/...`;
`bazel test //workers/download/...`. The edit is `;` appended to
web/shared/lib/markdown/markedRenderer.ts and to workers/download/src/index.ts.

## What each difference is

Every measurement below is from the median run's log of its cell at
83a141e/e864cab -- the run whose wall is the table's median -- unless it
names another run; a figure "at f123f53" is from the median run's log of
the same cell in the first run; a range in parentheses is the table's
minimum and maximum.

**Typecheck everything, cold.** typecheck.sh builds `@lovablelabs/agent-sdk`,
compiles the paraglide messages and runs `tsc --noEmit --incremental false`
over the 22 tsconfig.json files in its list at 83a141e, one process each, in
sequence: 30.0 s. Bazel runs 16044 actions (12725 internal, 3319 in
sandboxes) with a critical path of 310.9 s. The internal actions are the
importers' `node_modules` links: a target's npm packages are symlinks into
pnpm's virtual store, one `NpmStore` copy per package version shared by
every target that resolves it, where at f123f53 every target copied its own
tree from the pnpm store (5332 actions, 1095 in sandboxes, seven tree copies
of 128-194 s each, 812.7 s). The long action is `TsgoDeclare //web:web`,
122 s (213 s at f123f53): under `--declarations=tsgo` the check of a
program is its declaration emit, tsgo over the staged store trees with
`--explainFiles` (ts/private/actions/tsgo.bzl checks that listing against
the ownership manifest), so web is checked and its declarations written
where typecheck.sh only checks. The cold build also compiles the
`oxc-bazel` tool from Rust source and builds the Go toolchain it fetched
(`GoToolchainBinaryBuild`, 27 s), once per output base.

**Typecheck everything, warm.** typecheck.sh keeps no state (`--incremental
false`) and repeats the cold row: 25.9 s. Bazel runs `1 process: 1
internal` with a critical path of 0.02 s: 3.7 s, the analysis of 424
targets. At f123f53 the same cell re-checked 327 actions against the trees'
millions of files and took 58.9 s.

**Typecheck everything, a fresh output base over a populated disk cache.**
The shape of a CI runner with a shared cache. `16044 processes: 3319 disk
cache hit, 12725 internal`: every action the cold cell ran in a sandbox is
a cache hit -- the store copies, the compiles, the declares, the Go and
Rust tools -- and none executes; the 72.5 s are the analysis, the fetches
and the links, critical path 17.3 s. At f123f53 the 140 tree copies carried
`no-cache` and executed again in every fresh output base: 400.7 s. The
checkout has no counterpart: its caches are the vitest cache and
typecheck.sh's own outputs.

**Test everything, cold.** The checkout runs 31 rows -- 30 of them vitest
4.1.5; 284 files and 5677 tests pass -- in sequence, each with its own node
and pnpm start: 125.0 s. Two rows exit 1 in every
run, workers/o11y-tail-worker and workers/api-gateway: every test file of
theirs fails at collection with `TypeError: Cannot read properties of
undefined (reading 'config')`. Since b05ba7e declared
`@vitest/coverage-istanbul` in these two manifests -- neither declares
`vitest` -- pnpm links the peer's `vitest` bin into the member's
`node_modules/.bin`, one instance, while the member's files resolve
`vitest` from the root's `node_modules`, another; at f9fd041 the member had
no `.bin/vitest` and the root's ran both sides. Their Bazel targets pass:
the store gives a target one `vitest`. Bazel runs 15776 actions (12458
internal, 3362 in sandboxes) for 44 test targets; the tests' own times sum
to 270.2 s (proxy-worker2's 58.1 s the longest) and the critical path is
379.1 s: the store copies, the compiles and declares of everything the
tests need come first, `TsgoDeclare //web:web` 168 s on this lane. The
checkout's rows are what CI runs; the 44 targets are every `ts_test`
outside the exclusions, CI row or not. In run 3 proxy-worker2's test failed
after 108.1 s (its 3.3 MB of output is over Bazel's 1 MB stderr cap and not
in the log) after passing in runs 1 and 2 in 55.4 and 58.1 s; Bazel never
caches a failure, so run 3's warm and cached test cells ran it again -- the
table's maxima of 119.1 s and 210.2 s, exit 3.

**Test everything, warm.** vitest keeps no result cache and runs the rows
again: 126.1 s, the same two rows exit 1. Bazel runs none: `Executed 0 out
of 44 tests`, `1 process: 44 action cache hit, 1 internal`, 10.3 s (37.2 s
at f123f53).

**Test everything, a fresh output base over a populated disk cache.**
`15776 processes: 3362 disk cache hit, 12458 internal`, no test run: 72.4 s,
critical path 14.3 s. At f123f53 the 127 tree copies the cache never held
made it 321.0 s.

**One-line change in web, re-check.** `tsc -p web --noEmit` checks the whole
web program with no state: 15.6 s. `bazel build //web/...` re-runs the
three sandboxed actions on `//web:web` whose inputs changed -- `TsConfig`,
the `TsEmit` compile and `TsgoDeclare` at 121 s -- and hits the action cache
for the other 13: the declarations a `;` produces are byte-identical, so
nothing downstream re-runs; 190.9 s (139.5-244.5; 184.5 s at f123f53). The
target is the unit: one line in web re-checks and re-emits web, and the emit
is the cost tsc's `--noEmit` never pays.

**One-line change in web, re-test the affected suites.** The checkout's way
does not finish: `vitest run --changed` computes the affected set over web's
test files and dies at V8's heap limit (`FATAL ERROR: Reached heap limit`,
SIGABRT, exit 134) in every run, after 62.4 s (46.0-91.9); nothing in the
checkout raises the heap. `bazel test //web/...` runs the three test targets
whose inputs changed: `web_test` in 551.2 s (red, one test of three; its
output is over Bazel's 1 MB stderr cap and not in the log),
`node_tooling_test` 15.9 s, `scripts_lib_test` 0.7 s, after the same
re-emit of web on the test lane (`TsgoDeclare //web:web` 159 s); 725.0 s
(744.3 s at f123f53). A target is the unit of re-testing, so a one-line
change in web runs web's whole suite.

**One-line change in a leaf worker, re-test.** The checkout runs vitest in
workers/download: one file, 76 tests, 299 ms, 1.5 s with pnpm's start; CI
typechecks no worker (lint-js.yml generates `worker-configuration.d.ts` for
type-aware oxlint alone, and typecheck.sh's list of 22 tsconfigs at 83a141e
holds none). Bazel runs six sandboxed actions -- among them `TsCodegen` for
the worker's types and the test itself at 0.6 s -- and hits the action cache
for two: 7.8 s (9.2 s at f123f53), of which the type check is work the
checkout's CI never does.

**What a build costs the machine besides itself.** During the cold Bazel
check (392.0 s) the runner's own processes used 2760.2 CPU-seconds, the
kernel's threads 510.7 and the security sensor 447.9; during typecheck.sh
(30.0 s) the kernel 20.9 and the sensor 9.2. The threads copy and encrypt
what Bazel writes; the sensor inspects what it executes and opens. At
f123f53 the same cell cost 5751.8, 1108.9 and 1401.2: the trees copied per
target were most of what the threads and the sensor saw.
