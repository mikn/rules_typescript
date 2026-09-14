# Benchmark

The same work done two ways on one checkout of the Lovable monorepo at parity,
measured three times: at commit f9fd041 of the trial with this ruleset at
f123f53, at 613f2b2 with 5049c1b, and at da0b73d with 801952e (TypeScript
7.0.2, node 24.14.1, pnpm 11.5.3, Bazel 9.2.0 each time). The checkout's own
commands as CI and a developer run them, and `bazel build //...` /
`bazel test //...` with `--@rules_typescript//ts:declarations=tsgo` and a disk
cache; the first two runs passed `--norun_validations`, when the lint was the
one validation action, the third
`--@rules_typescript//ts:lint=@rules_typescript//ts:no_lint`, which turns the
linter off alone -- `TsgoCheck` is a validation action now, and
`--norun_validations` would skip the check. Four cache states, three runs per
cell, the median with its spread; `tools/bench_parity.sh` ran all three
(`OTHER_CORES=2 REDO=6 SYSTEM_PROCS=falcon-sensor-bpf`, the excluded targets
below). `BAZEL_FLAGS` adds a consumer's build options to every Bazel cell
after the runner's flag set; the three runs passed none. A target red at the
parity proof is left out of the test-everything
cells on both sides, together with the checkout rows that run the same files:
Bazel never caches a failed test, so a red target would put its own run into
every warm row. At f9fd041 thirteen `ts_test` targets were left out -- the
nine red at that proof and the four that share a CI row with one of them:
`//web:web_test`, `//web:node_tooling_test`, `//web:scripts_lib_test`,
`//packages/ui:ui_test`, `//packages/applocal-mcp:applocal-mcp_test`,
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
and `//workers/rudderstack-proxy/test:test_test`. At da0b73d eleven: the same
six (`web_test` red on one 5000 ms case, `node_tooling_test` on two 5000 ms
cases beside web_test's forks and green alone, `ui_test` on the chart re-bless
and one 5000 ms case, the other three green and in that row),
`//desktop:desktop_test`, `//workers/mcp-server/test:test_test`,
`//workers/proxy-worker2/test:test_test` (two 5000 ms cases and one 10000 ms
hook at load 25-32, green alone; cf-workers-test.yml's step in that directory
dropped with it), `//npm-packages/lovite:lovite_test` and
`//workers/rudderstack-proxy/test:test_test`. Every red at the last two proofs
is an owner's row or a load row, none the ruleset's. Nothing was red at either
of those proofs' builds (`bazel build //...` exit 0 over 426 targets at
613f2b2, over 426 at da0b73d), so every `//...` cell holds the whole tree.
The test lane builds the excluded targets once, after its warm row (106.7 s at
f9fd041, 24.6 s at 613f2b2, 38.9 s at da0b73d), so the edit rows start from a
built tree as the checkout's do. The edit rows' patterns stay whole:
`//web/...` holds `web_test`.

The machine: 22 cores, 62 GB, btrfs on dm-crypt; 54-70 GB of swap in use
throughout the first run, 7-20 GB throughout the second, 33-42 GB throughout
the third. A cell starts when processes outside the runner's tree use under 2
cores over 3 s, and a cell during which they averaged 2 or more is redone with
its segment; the kernel's threads and the security sensor (`falcon-sensor-bpf`,
whose CPU follows the benchmark's own exec and file activity) are neither
side. Every kept cell's other work is in the table. At f9fd041 one cell of
the 51 was redone (run 3's `vitest run --changed`, 5.97 cores of other work).
At 613f2b2 three cells of the 51 were redone with their segments (run 1's
`tsc -p web` at 2.98 cores, run 2's `bazel build //web/...` at 3.50 and
run 2's cached `bazel build //...` at 2.02). At da0b73d one cell of the 54
(run 1's `bazel build //web:web` at 4.68 cores, with its web-edit segment);
the gate waited up to 76 s for other work to fall under 2 cores.

## The table

Seconds, the median of three runs with the minimum and maximum; other work in
cores, the median run's. An exit code stands where it is not 0: the failing
step's for the checkout, Bazel's for Bazel. The column pairs are the runs at
f9fd041/f123f53, 613f2b2/5049c1b and da0b73d/801952e; a cell a run did not
have is empty.

| Work | Cache state | Checkout, f9fd041 | Bazel, f123f53 | Checkout, 613f2b2 | Bazel, 5049c1b | Checkout, da0b73d | Bazel, 801952e | Other work at f9fd041 (checkout / Bazel) | Other work at 613f2b2 (checkout / Bazel) | Other work at da0b73d (checkout / Bazel) |
|---|---|---|---|---|---|---|---|---|---|---|
| Typecheck everything | cold | 31.2 (26.9-31.5) | 812.7 (791.6-887.3) | 23.5 (23.0-23.6) | 356.7 (351.7-357.2) | 26.0 (25.4-27.1) | 347.4 (346.7-353.0) | 0.75 / 0.78 | 0.43 / 1.63 | 0.54 / 0.49 |
| Typecheck everything | warm, nothing changed | 26.7 (25.7-31.9) | 58.9 (48.6-73.8) | 23.0 (22.7-24.6) | 3.5 (3.4-3.9) | 24.3 (23.1-24.3) | 4.5 (4.3-4.5) | 1.00 / 0.96 | 0.65 / 0.26 | 0.93 / 0.30 |
| Typecheck everything | fresh output base, populated disk cache |  | 400.7 (376.6-421.4) |  | 68.6 (65.7-70.1) |  | 73.1 (67.5-74.5) | 0.45 | 0.08 | 0.52 |
| Test everything | cold | 131.9 (129.5-133.4) | 799.8 (761.7-833.6) | 115.2 (115.0-115.5), exit 1/1 | 422.7 (419.4-442.6) | 74.6 (72.7-74.9) | 364.6 (344.6-370.9) | 0.57 / 1.27 | 0.38 / 0.27 | 0.49 / 1.80 |
| Test everything | warm, nothing changed | 125.2 (125.1-126.3) | 37.2 (35.2-43.7) | 111.5 (111.3-111.5), exit 1/1 | 3.6 (3.3-3.7) | 75.0 (73.6-76.3) | 3.7 (3.2-3.7) | 0.53 / 0.67 | 0.37 / 0.16 | 1.02 / 0.94 |
| Test everything | fresh output base, populated disk cache |  | 321.0 (297.7-338.0) |  | 66.3 (66.1-70.2) |  | 71.2 (68.2-74.9) | 0.36 | 0.13 | 0.52 |
| One-line change in web, re-check the target alone | warm |  |  |  |  |  | 34.4 (25.0-51.8) |  |  | 0.44 |
| One-line change in web, re-check | warm | 19.5 (18.4-20.0) | 184.5 (146.1-186.2) | 13.2 (12.5-13.6) | 144.9 (133.5-145.3) | 14.9 (12.8-15.6) | 106.8 (104.7-107.6) | 1.70 / 1.07 | 0.55 / 1.41 | 1.52 / 0.76 |
| One-line change in web, re-test | warm | 48.1 (47.4-64.3), exit 134 | 744.3 (743.9-752.9), exit 3 | 49.3 (43.3-55.5), exit 134 | 610.7 (608.7-639.9), exit 3 | 466.9 (458.8-469.6), exit 1 | 588.6 (582.3-600.8), exit 3 | 0.54 / 0.75 | 0.29 / 0.42 | 1.22 / 1.57 |
| One-line change in a leaf worker, re-test | warm | 1.7 (1.5-2.1) | 9.2 (8.0-9.7) | 1.1 (1.1-1.1) | 5.5 (5.4-5.5) | 1.7 (1.6-1.8) | 6.1 (6.0-6.8) | 0.48 / 0.57 | 0.28 / 0.36 | 0.41 / 0.67 |

A cold Bazel cell also builds the ruleset's tools -- the Go toolchain's
builder and standard library (`GoToolchainBinaryBuild`, `GoStdlib`), the Go
tools they compile and oxc's crates (`Compiling Rust`) -- work no checkout
command does and the comparison leaves out, so the cold comparison of the
build itself is the fresh-output-base rows, 73.1 s and 71.2 s, where those
actions are disk cache hits like everything else.

The checkout's way: `.github/scripts/typecheck.sh`; the CI `run:` lines that
run TypeScript tests outside the excluded rows (11 at f9fd041, 12 at 613f2b2,
12 at da0b73d) plus cf-workers-test.yml's step in the worker directories CI
tests outside them (21 at 613f2b2, 20 at da0b73d), one after another;
`tsc -p web --noEmit`; CI's web row, `pnpm --filter=web run test:run`
(test.yml:1105) -- the first two runs ran
`pnpm --filter=web run test:run --changed`, a command CI does not run;
cf-workers-test.yml's step in workers/download. Bazel's: `bazel build //...`;
`bazel test //...` minus the excluded labels; `bazel build //web:web` after
the edit and then `bazel build //web/...` on the same output base -- in one
`bazel build //web/...` the target's check, emit and declare run side by side
after `TsConfig`, in the two cells the declare follows the check, so the two
walls' sum is an upper bound on one `//web/...` build's wall, over it by about
the shorter cell; the first two runs ran `bazel build //web/...` alone;
`bazel test //web/...`; `bazel test //workers/download/...`. The edit is `;`
appended to web/shared/lib/markdown/markedRenderer.ts and to
workers/download/src/index.ts.

## What each difference is

Every measurement below is from the median run's log of its cell at
da0b73d/801952e -- the run whose wall is the table's median -- unless it
names another run; a figure "at 5049c1b" is from the median run's log of the
same cell in the second run, a figure "at f123f53" from the first; a range in
parentheses is the table's minimum and maximum.

**Typecheck everything, cold.** typecheck.sh builds `@lovablelabs/agent-sdk`,
compiles the paraglide messages and runs `tsc --noEmit --incremental false`
over the 22 tsconfig.json files in its list at da0b73d, one process each, in
sequence: 26.0 s. Bazel runs 16201 actions (12832 internal, 3369 in
sandboxes) with a critical path of 271.78 s. The internal actions are the
importers' `node_modules` links: a target's npm packages are symlinks into
pnpm's virtual store, one `NpmStore` copy per package version shared by
every target that resolves it, where at f123f53 every target copied its own
tree from the pnpm store (5332 actions, 1095 in sandboxes, seven tree copies
of 128-194 s each, 812.7 s). Every program is checked by `TsgoCheck` under
`--noEmit` -- tsgo over a program root tsaction lays out from the action's
inputs, the srcs, the tsconfig chain, the deps' declarations and the
importers' `node_modules` links, with `--explainFiles`, whose listing
tsaction checks against the ownership manifest -- and `TsgoDeclare` writes a
program's declarations only where a dependent's compile reads them, so a
leaf's build runs the check alone; at 5049c1b the one tsgo action per target
was the declare, and the check was its declaration emit. The long actions on
this path: `TsgoDeclare //web:web`, 73 s in the progress lines (110 s at
5049c1b, 213 s at f123f53), read by web's worker_entry, node_tooling and
node_tooling_test under their own tsconfigs and by `web_test`'s check through
`//workers/web-proxy:web-proxy`; then `TsgoCheck //web:node_tooling_test`
48 s, `//web:node_tooling` 46 s and `//web:web_test` 42 s, each a program
over web's sources, and `TsgoCheck //web:web` 25 s. The cold build also
compiles the `oxc-bazel` tool from Rust source and builds the Go toolchain it
fetched (`GoToolchainBinaryBuild`, 26 s), once per output base.

**Typecheck everything, warm.** typecheck.sh keeps no state (`--incremental
false`) and repeats the cold row: 24.3 s. Bazel runs `1 process: 1
internal` with a critical path of 0.02 s: 4.5 s, the analysis of 426
targets (3.5 s at 5049c1b; at f123f53 the same cell re-checked 327 actions
against the trees' millions of files and took 58.9 s).

**Typecheck everything, a fresh output base over a populated disk cache.**
The shape of a CI runner with a shared cache. `16201 processes: 3369 disk
cache hit, 12832 internal`: every action the cold cell ran in a sandbox is
a cache hit -- the store copies, the checks, the compiles, the declares, the
Go and Rust tools -- and none executes; the 73.1 s are the analysis, the
fetches and the links, critical path 18.94 s (68.6 s and 14.4 s at 5049c1b;
at f123f53 the 140 tree copies carried `no-cache` and executed again in every
fresh output base: 400.7 s). The checkout has no counterpart: its caches are
the vitest cache and typecheck.sh's own outputs.

**Test everything, cold.** The checkout runs 32 rows -- 30 of them vitest
4.1.5; 176 files and 3357 tests pass -- in sequence, each with its own node
and pnpm start: 74.6 s, every row exit 0. At 613f2b2 two rows exited 1 in
every run, workers/o11y-tail-worker and workers/api-gateway: every test file
of theirs failed at collection with `TypeError: Cannot read properties of
undefined (reading 'config')`, because b05ba7e had declared
`@vitest/coverage-istanbul` in members that declare no `vitest`, so pnpm
linked the peer's `vitest` bin into the member's `node_modules/.bin` while
the files resolved the root's; d980cc4 and bee7d0b declare `vitest` and
`@cloudflare/vitest-pool-workers` beside the provider, and both rows pass.
proxy-worker2's row is out with its red target, so the checkout runs one
worker directory fewer than at 613f2b2 (33 rows, 115.2 s). Bazel runs 15932
actions (12568 internal, 3409 in sandboxes) for 45 test targets; the tests'
own times sum to 154.0 s (lovable-mcp-js's 14.0 s the longest) and the
critical path is 288.18 s: the store links, the checks and the compiles of
everything the tests need come first, `TsgoDeclare //web:web` 80 s on this
lane, read by `//workers/web-proxy:web-proxy` for its test; 364.6 s (422.7 s
at 5049c1b, 799.8 s at f123f53). The checkout's rows are what CI runs; the
45 targets are every `ts_test` outside the exclusions, CI row or not.

**Test everything, warm.** vitest keeps no result cache and runs the rows
again: 75.0 s. Bazel runs none: `Executed 0 out of 45 tests`, `1 process:
45 action cache hit, 1 internal`, 3.7 s (3.6 s at 5049c1b, 37.2 s at
f123f53).

**Test everything, a fresh output base over a populated disk cache.**
`15932 processes: 3409 disk cache hit, 12568 internal`, no test run: 71.2 s,
critical path 13.07 s (66.3 s at 5049c1b; at f123f53 the 127 tree copies the
cache never held made it 321.0 s).

**One-line change in web, re-check the target alone.** `bazel build //web:web`
after the edit runs the three sandboxed actions of `//web:web` whose inputs
hold the file -- `TsConfig`, `TsgoCheck` (19 s in the progress lines) and the
`TsEmit` compile -- and no `TsgoDeclare`: `4 processes: 1 internal, 3
linux-sandbox`, 34.4 s (25.0-51.8; run 2 ran under 1.97 cores of other work,
run 1 in 25.0 s under 0.44). `tsc -p web --noEmit` checks the same sources
with no state in 14.9 s; the rest of the Bazel cell is tsaction's layout of
the program root, the sandbox and the analysis. At 5049c1b there was no such
cell: the target's one tsgo action was `TsgoDeclare`, so `bazel build
//web:web` cost what `//web/...` did.

**One-line change in web, re-check.** `bazel build //web/...` on the same
output base then runs three sandboxed actions and hits the action cache for
14: `TsgoDeclare //web:web` (41 s in the progress lines), which
worker_entry, node_tooling and node_tooling_test read under their own
tsconfigs, and `TsConfig` and `TsgoCheck //web:web_test` (11 s), which reads
web's declarations through `//workers/web-proxy:web-proxy`, a dep of the test
under another ts_config that depends on `//web:web`; critical path 104.89 s,
106.8 s (104.7-107.6). The declarations a `;` produces are byte-identical,
so the three readers' own checks and compiles hit the cache. At 5049c1b the
cell was the whole re-check -- `TsConfig`, `TsEmit` and `TsgoDeclare
//web:web` at 135 s, the declare being the check -- 144.9 s (133.5-145.3;
184.5 s at f123f53).

**One-line change in web, re-test.** The checkout's cell is CI's row,
`pnpm --filter=web run test:run` (test.yml:1105): one `vitest run` over web's
project and its member projects, 2372 files and 26797 tests, `Duration
463.86s`; 466.9 s (458.8-469.6), exit 1 on 6 files and 15 tests, the
checkout's own reds -- among them the two cases that time out under Bazel
below and one of plugins/i18n/emit's -- and no heap limit. At 613f2b2 the cell
ran `vitest run --changed`, a command CI does not run, which computes the
affected set over web's test files and died at V8's heap limit (`FATAL
ERROR: Reached heap limit`, SIGABRT, exit 134) in every run after 49.3 s
(43.3-55.5): not a number. `bazel test //web/...` runs 12 sandboxed actions
with 13 action cache hits -- among them web's `TsConfig`, `TsgoCheck` (25 s)
and `TsEmit` again on this lane, `TsgoDeclare //web:web` (102 s), then
`web_test`'s `TsConfig` and `TsgoCheck` (23 s), and the three tests:
`web_test` 448.9 s (red on its two 5000 ms cases, goblinStore and
system-status.server, the load rows the exclusion names; vitest's own
`Duration 441.12s` over 2235 files and 25229 tests), `node_tooling_test`
18.5 s, `scripts_lib_test` 1.0 s; critical path 560.04 s, 588.6 s
(582.3-600.8). At 5049c1b the same cell re-emitted web through `TsgoDeclare
//web:web` at 127 s and ran the test in 474.2 s: 610.7 s (608.7-639.9;
744.3 s at f123f53). A target is the unit of re-testing, so a one-line change
in web runs web's whole suite; the test's own check no longer waits for its
package's declare -- `:web` joins its program as sources -- but for web's
declarations through web-proxy.

**One-line change in a leaf worker, re-test.** The checkout runs vitest in
workers/download: one file, 76 tests, 222 ms, 1.7 s with pnpm's start; CI
typechecks no worker (lint-js.yml generates `worker-configuration.d.ts` for
type-aware oxlint alone, and typecheck.sh's list of 22 tsconfigs at da0b73d
holds none). Bazel runs seven sandboxed actions -- among them `TsCodegen`
for the worker's types and the test itself at 0.9 s -- and hits the action
cache for two: 6.1 s (5.5 s at 5049c1b, 9.2 s at f123f53), of which the type
check is work the checkout's CI never does.

**What a build costs the machine besides itself.** During the cold Bazel
check (347.4 s) the runner's own processes used 2866.3 CPU-seconds, the
kernel's threads 389.4 and the security sensor 392.0; during typecheck.sh
(26.0 s) the kernel 4.7 and the sensor 8.8. The threads copy and encrypt
what Bazel writes; the sensor inspects what it executes and opens. At
5049c1b the same cells cost 2576.9, 241.8 and 406.9, and 3.3 and 8.5; at
f123f53 the cold check cost 5751.8, 1108.9 and 1401.2: the trees copied per
target were most of what the threads and the sensor saw.
