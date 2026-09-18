# Benchmark

The same work done two ways on one checkout of the Lovable monorepo at parity,
measured four times: at commit f9fd041 of the trial with this ruleset at
f123f53, at 613f2b2 with 5049c1b, at da0b73d with 801952e and at d7346c6 with
f89ce47 (TypeScript 7.0.2, node 24.14.1, pnpm 11.5.3, Bazel 9.2.0 each time).
The checkout's own commands as CI and a developer run them, and
`bazel build //...` / `bazel test //...` with
`--@rules_typescript//ts:declarations=tsgo` and a disk cache; the first two
runs passed `--norun_validations`, when the lint was the one validation
action, the last two
`--@rules_typescript//ts:lint=@rules_typescript//ts:no_lint`, which turns the
linter off alone -- `TsgoCheck` is a validation action now, and
`--norun_validations` would skip the check. `BAZEL_FLAGS` adds a consumer's
build options to every Bazel cell after the runner's flag set; the fourth run
passed two through it: `--extra_toolchains=` naming
`@rules_typescript//ts/tools/tsaction:source_toolchain` and
`@rules_typescript//tools/launcher:source_toolchain_linux_amd64`, the
ruleset's Go tools built from source -- the tools release
`ts/private/tools_lock.bzl` names does not exist yet, and the cold cells
compile the same tools the earlier runs compiled as targets (3369 sandboxed
actions in the cold check at da0b73d and at d7346c6) -- and
`--@rules_typescript//ts:checkers=16`, sixteen checker threads for every
`TsgoCheck` and `TsgoDeclare` on this 22-core machine against tsgo's own
four. Four cache states, three runs per cell, the median with its spread;
`tools/bench_parity.sh` ran all four
(`OTHER_CORES=2 REDO=6 SYSTEM_PROCS=falcon-sensor-bpf`, the excluded targets
below). A target red at the parity proof is left out of the test-everything
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
`//workers/rudderstack-proxy/test:test_test`. At d7346c6 the same eleven
(`web_test` red on one 5000 ms case, `node_tooling_test` on one 5000 ms case
beside web_test's forks and green alone, `ui_test` on the chart re-bless and
one 5000 ms case, `proxy-worker2` on four 5000 ms cases and one 10000 ms hook
at load 17-37 and green alone, the other three of test.yml:1105 green and in
that row). Every red at the last three proofs is an owner's row or a load
row, none the ruleset's. Nothing was red at any of those proofs' builds
(`bazel build //...` exit 0 over 426 targets at 613f2b2, over 426 at da0b73d,
over 426 at d7346c6), so every `//...` cell holds the whole tree. The test
lane builds the excluded targets once, after its warm row (106.7 s at
f9fd041, 24.6 s at 613f2b2, 38.9 s at da0b73d, 53.3 s at d7346c6, where the
twelve targets' checks run one after another under the sixteen-cpu
requirement below), so the edit rows start from a built tree as the
checkout's do. The edit rows' patterns stay whole: `//web/...` holds
`web_test`.

The machine: 22 cores, 62 GB, btrfs on dm-crypt; 54-70 GB of swap in use
throughout the first run, 7-20 GB throughout the second, 33-42 GB throughout
the third, 42-57 GB throughout the fourth. A cell starts when processes
outside the runner's tree use under 2 cores over 3 s, and a cell during which
they averaged 2 or more is redone with its segment; the kernel's threads and
the security sensor (`falcon-sensor-bpf`, whose CPU follows the benchmark's
own exec and file activity) are neither side. Every kept cell's other work is
in the table. At f9fd041 one cell of the 51 was redone (run 3's
`vitest run --changed`, 5.97 cores of other work). At 613f2b2 three cells of
the 51 were redone with their segments (run 1's `tsc -p web` at 2.98 cores,
run 2's `bazel build //web/...` at 3.50 and run 2's cached `bazel build //...`
at 2.02). At da0b73d one cell of the 54 (run 1's `bazel build //web:web` at
4.68 cores, with its web-edit segment); the gate waited up to 76 s for other
work to fall under 2 cores. At d7346c6 two cells of the 54 (run 1's warm
`bazel test //...` at 2.08 cores, with its test lane; run 2's `tsc -p web` at
3.81, with its web-edit segment); the gate waited up to 21 s.

## The table

Seconds, the median of three runs with the minimum and maximum; other work in
cores, the median run's. An exit code stands where it is not 0: the failing
step's for the checkout, Bazel's for Bazel. The column pairs are the runs at
f9fd041/f123f53, 613f2b2/5049c1b, da0b73d/801952e and d7346c6/f89ce47; a
cell a run did not have is empty.

| Work | Cache state | Checkout, f9fd041 | Bazel, f123f53 | Checkout, 613f2b2 | Bazel, 5049c1b | Checkout, da0b73d | Bazel, 801952e | Checkout, d7346c6 | Bazel, f89ce47 | Other work at f9fd041 (checkout / Bazel) | Other work at 613f2b2 (checkout / Bazel) | Other work at da0b73d (checkout / Bazel) | Other work at d7346c6 (checkout / Bazel) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Typecheck everything | cold | 31.2 (26.9-31.5) | 812.7 (791.6-887.3) | 23.5 (23.0-23.6) | 356.7 (351.7-357.2) | 26.0 (25.4-27.1) | 347.4 (346.7-353.0) | 25.3 (25.3-26.8) | 346.1 (345.6-348.2) | 0.75 / 0.78 | 0.43 / 1.63 | 0.54 / 0.49 | 0.44 / 0.46 |
| Typecheck everything | warm, nothing changed | 26.7 (25.7-31.9) | 58.9 (48.6-73.8) | 23.0 (22.7-24.6) | 3.5 (3.4-3.9) | 24.3 (23.1-24.3) | 4.5 (4.3-4.5) | 23.7 (22.4-23.9) | 3.7 (3.6-4.1) | 1.00 / 0.96 | 0.65 / 0.26 | 0.93 / 0.30 | 0.60 / 0.47 |
| Typecheck everything | fresh output base, populated disk cache |  | 400.7 (376.6-421.4) |  | 68.6 (65.7-70.1) |  | 73.1 (67.5-74.5) |  | 76.4 (70.5-78.3) | 0.45 | 0.08 | 0.52 | 0.66 |
| Test everything | cold | 131.9 (129.5-133.4) | 799.8 (761.7-833.6) | 115.2 (115.0-115.5), exit 1/1 | 422.7 (419.4-442.6) | 74.6 (72.7-74.9) | 364.6 (344.6-370.9) | 74.0 (73.9-78.5) | 353.0 (325.6-354.9) | 0.57 / 1.27 | 0.38 / 0.27 | 0.49 / 1.80 | 0.49 / 1.34 |
| Test everything | warm, nothing changed | 125.2 (125.1-126.3) | 37.2 (35.2-43.7) | 111.5 (111.3-111.5), exit 1/1 | 3.6 (3.3-3.7) | 75.0 (73.6-76.3) | 3.7 (3.2-3.7) | 74.2 (72.5-76.6) | 3.6 (3.0-3.7) | 0.53 / 0.67 | 0.37 / 0.16 | 1.02 / 0.94 | 0.62 / 0.28 |
| Test everything | fresh output base, populated disk cache |  | 321.0 (297.7-338.0) |  | 66.3 (66.1-70.2) |  | 71.2 (68.2-74.9) |  | 67.6 (66.4-68.9) | 0.36 | 0.13 | 0.52 | 0.77 |
| One-line change in web, re-check the target alone | warm |  |  |  |  |  | 34.4 (25.0-51.8) |  | 23.6 (20.1-32.9) |  |  | 0.44 | 0.52 |
| One-line change in web, re-check | warm | 19.5 (18.4-20.0) | 184.5 (146.1-186.2) | 13.2 (12.5-13.6) | 144.9 (133.5-145.3) | 14.9 (12.8-15.6) | 106.8 (104.7-107.6) | 13.8 (13.0-14.3) | 82.1 (81.3-85.2) | 1.70 / 1.07 | 0.55 / 1.41 | 1.52 / 0.76 | 0.94 / 0.44 |
| One-line change in web, re-test | warm | 48.1 (47.4-64.3), exit 134 | 744.3 (743.9-752.9), exit 3 | 49.3 (43.3-55.5), exit 134 | 610.7 (608.7-639.9), exit 3 | 466.9 (458.8-469.6), exit 1 | 588.6 (582.3-600.8), exit 3 | 458.9 (454.7-459.7), exit 1 | 542.8 (540.6-561.7), exit 3 | 0.54 / 0.75 | 0.29 / 0.42 | 1.22 / 1.57 | 0.80 / 0.75 |
| One-line change in a leaf worker, re-test | warm | 1.7 (1.5-2.1) | 9.2 (8.0-9.7) | 1.1 (1.1-1.1) | 5.5 (5.4-5.5) | 1.7 (1.6-1.8) | 6.1 (6.0-6.8) | 2.2 (1.5-2.3) | 6.3 (6.1-6.6) | 0.48 / 0.57 | 0.28 / 0.36 | 0.41 / 0.67 | 0.25 / 0.30 |

A cold Bazel cell also builds the ruleset's tools -- the Go toolchain's
builder and standard library (`GoToolchainBinaryBuild`, `GoStdlib`), the Go
tools they compile and oxc's crates (`Compiling Rust`) -- work no checkout
command does and the comparison leaves out, so the cold comparison of the
build itself is the fresh-output-base rows, 76.4 s and 67.6 s at d7346c6
(73.1 s and 71.2 s at da0b73d), where those actions are disk cache hits like
everything else.

The checkout's way: `.github/scripts/typecheck.sh`; the CI `run:` lines that
run TypeScript tests outside the excluded rows (11 at f9fd041, 12 at 613f2b2,
12 at da0b73d, 12 at d7346c6) plus cf-workers-test.yml's step in the worker
directories CI tests outside them (21 at 613f2b2, 20 at da0b73d, 20 at
d7346c6), one after another; `tsc -p web --noEmit`; CI's web row,
`pnpm --filter=web run test:run` (test.yml:1105) -- the first two runs ran
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
d7346c6/f89ce47 -- the run whose wall is the table's median -- unless it
names another run; a figure "at 801952e" is from the median run's log of the
same cell in the third run, a figure "at 5049c1b" from the second, a figure
"at f123f53" from the first; a range in parentheses is the table's minimum
and maximum.

**Typecheck everything, cold.** typecheck.sh builds `@lovablelabs/agent-sdk`,
compiles the paraglide messages and runs `tsc --noEmit --incremental false`
over the 22 tsconfig.json files in its list at d7346c6, one process each, in
sequence: 25.3 s. Bazel runs 16197 actions (12828 internal, 3369 in
sandboxes) with a critical path of 255.48 s. The internal actions are the
importers' `node_modules` links: a target's npm packages are symlinks into
pnpm's virtual store, one `NpmStore` copy per package version shared by
every target that resolves it, where at f123f53 every target copied its own
tree from the pnpm store (5332 actions, 1095 in sandboxes, seven tree copies
of 128-194 s each, 812.7 s). Every program is checked by `TsgoCheck` under
`--noEmit` -- tsgo over a program root tsaction lays out from the action's
inputs, the srcs, the tsconfig chain, the deps' declarations and the
importers' `node_modules` links, with `--explainFiles`, whose listing
tsaction checks against the ownership manifest, and `--checkers 16` -- and
`TsgoDeclare` writes a program's declarations only where a dependent's
compile reads them, so a leaf's build runs the check alone; at 5049c1b the
one tsgo action per target was the declare, and the check was its
declaration emit. Each tsgo action declares its sixteen cpus to Bazel
(`cpu:16`), which admits one at a time on 22 cores: the checks run one after
another, each with its threads, beside everything else. The long actions on
this path: `TsgoDeclare //web:web`, 62 s in the progress lines (73 s at
801952e, 110 s at 5049c1b, 213 s at f123f53), read by web's worker_entry,
node_tooling and node_tooling_test under their own tsconfigs and by
`//workers/web-proxy:web-proxy`'s declare, whose declarations `web_test`'s
check reads; then `TsgoCheck //web:web` 21 s, `//web:web_test` 18 s,
`//web:node_tooling` 15 s, `//web:node_tooling_test` 6 s and
`//web:worker_entry` 5 s (at 801952e, with four threads each: node_tooling_test
48 s, node_tooling 46 s, web_test 42 s, web 25 s). The cold build also
compiles the `oxc-bazel` tool from Rust source, the ruleset's Go tools and
the Go toolchain it fetched (`GoToolchainBinaryBuild`, 24 s), once per output
base.

**Typecheck everything, warm.** typecheck.sh keeps no state (`--incremental
false`) and repeats the cold row: 23.7 s. Bazel runs `1 process: 1
internal` with a critical path of 0.01 s: 3.7 s, the analysis of 426
targets (4.5 s at 801952e, 3.5 s at 5049c1b; at f123f53 the same cell
re-checked 327 actions against the trees' millions of files and took
58.9 s).

**Typecheck everything, a fresh output base over a populated disk cache.**
The shape of a CI runner with a shared cache. `16197 processes: 3369 disk
cache hit, 12828 internal`: every action the cold cell ran in a sandbox is
a cache hit -- the store copies, the checks, the compiles, the declares, the
Go and Rust tools -- and none executes; the 76.4 s are the analysis, the
fetches and the links, critical path 23.90 s (73.1 s and 18.94 s at 801952e,
68.6 s and 14.4 s at 5049c1b; at f123f53 the 140 tree copies carried
`no-cache` and executed again in every fresh output base: 400.7 s). The
checkout has no counterpart: its caches are the vitest cache and
typecheck.sh's own outputs.

**Test everything, cold.** The checkout runs 32 rows -- 30 of them vitest
4.1.5; 176 files and 3357 tests pass -- in sequence, each with its own node
and pnpm start: 74.0 s, every row exit 0 (74.6 s at da0b73d). At 613f2b2 two
rows exited 1 in every run, workers/o11y-tail-worker and workers/api-gateway:
every test file of theirs failed at collection with `TypeError: Cannot read
properties of undefined (reading 'config')`, because b05ba7e had declared
`@vitest/coverage-istanbul` in members that declare no `vitest`, so pnpm
linked the peer's `vitest` bin into the member's `node_modules/.bin` while
the files resolved the root's; d980cc4 and bee7d0b declare `vitest` and
`@cloudflare/vitest-pool-workers` beside the provider, and both rows pass.
proxy-worker2's row is out with its red target since da0b73d, so the checkout
runs one worker directory fewer than at 613f2b2 (33 rows, 115.2 s). Bazel
runs 15928 actions (12564 internal, 3409 in sandboxes) for 45 test targets;
the tests' own times sum to 147.2 s (lovable-mcp-js's 13.4 s the longest) and
the critical path is 253.75 s: the store links, the checks and the compiles
of everything the tests need come first, `TsgoDeclare //web:web` 83 s on
this lane, read by `//workers/web-proxy:web-proxy` for its test; 353.0 s
(364.6 s at 801952e, 422.7 s at 5049c1b, 799.8 s at f123f53). The checkout's
rows are what CI runs; the 45 targets are every `ts_test` outside the
exclusions, CI row or not.

**Test everything, warm.** vitest keeps no result cache and runs the rows
again: 74.2 s. Bazel runs none: `Executed 0 out of 45 tests`, `1 process:
45 action cache hit, 1 internal`, 3.6 s (3.7 s at 801952e, 3.6 s at
5049c1b, 37.2 s at f123f53).

**Test everything, a fresh output base over a populated disk cache.**
`15928 processes: 3409 disk cache hit, 12564 internal`, no test run: 67.6 s,
critical path 12.50 s (71.2 s at 801952e, 66.3 s at 5049c1b; at f123f53 the
127 tree copies the cache never held made it 321.0 s).

**One-line change in web, re-check the target alone.** `bazel build //web:web`
after the edit runs the three sandboxed actions of `//web:web` whose inputs
hold the file -- `TsConfig`, `TsgoCheck` (13 s in the progress lines, sixteen
threads) and the `TsEmit` compile -- and no `TsgoDeclare`: `4 processes: 1
internal, 3 linux-sandbox`, 23.6 s (20.1-32.9). `tsc -p web --noEmit` checks
the same sources with tsgo's four threads and no state in 13.8 s; the rest
of the Bazel cell is tsaction's layout of the program root, the sandbox and
the analysis. At 801952e the check ran with four threads, 19 s in the
progress lines: 34.4 s (25.0-51.8). At 5049c1b there was no such cell: the
target's one tsgo action was `TsgoDeclare`, so `bazel build //web:web` cost
what `//web/...` did.

**One-line change in web, re-check.** `bazel build //web/...` on the same
output base then runs three sandboxed actions and hits the action cache for
14: `TsgoDeclare //web:web` (41 s in the progress lines), which
worker_entry, node_tooling and node_tooling_test read under their own
tsconfigs and `//workers/web-proxy:web-proxy`'s declare reads, and `TsConfig`
and `TsgoCheck //web:web_test` (11 s); critical path 80.17 s, 82.1 s
(81.3-85.2). The test's check holds none of web's declarations -- `:web`
joins its program as sources, on every path -- and ten of web-proxy's, a dep
of the test under another ts_config that depends on `//web:web`; web-proxy's
declare is an action-cache hit only once web's declare has re-produced its
outputs, so the check starts after the declare ends (in one build of this
cell at f89ce47 with a profile, the declare 61.7 s of which 58.0 s in tsgo,
the check 16.2 s of which 14.9 s). The declarations a `;` produces are
byte-identical, so the three readers' own checks and compiles hit the cache.
At 801952e the check read web's declarations itself, through web-proxy, and
both actions ran with four threads (the declare 41 s and the check 11 s in
the progress lines, critical path 104.89 s): 106.8 s (104.7-107.6). At
5049c1b the cell was the whole re-check -- `TsConfig`, `TsEmit` and
`TsgoDeclare //web:web` at 135 s, the declare being the check -- 144.9 s
(133.5-145.3; 184.5 s at f123f53).

**One-line change in web, re-test.** The checkout's cell is CI's row,
`pnpm --filter=web run test:run` (test.yml:1105): one `vitest run` over web's
project and its member projects, 2372 files and 26797 tests, `Duration
456.01s`; 458.9 s (454.7-459.7), exit 1 on 4 files and 13 tests, the
checkout's own reds (466.9 s at da0b73d, on 6 files and 15 tests) and no heap
limit. At 613f2b2 the cell ran `vitest run --changed`, a command CI does not
run, which computes the affected set over web's test files and died at V8's
heap limit (`FATAL ERROR: Reached heap limit`, SIGABRT, exit 134) in every
run after 49.3 s (43.3-55.5): not a number. `bazel test //web/...` runs 12
sandboxed actions with 13 action cache hits -- among them web's `TsConfig`,
`TsgoCheck` (16 s) and `TsEmit` again on this lane, `TsgoDeclare //web:web`
(60 s), then `web_test`'s `TsConfig` and `TsgoCheck`, and the three tests:
`web_test` 438.5 s, red -- its output, 1287190 bytes, is over Bazel's 1 MB
stdout cap (`--experimental_ui_max_stdouterr_bytes`) and not in the cell's
log; the same edit and test at f89ce47 in one stamped run: goblinStore alone,
`FAILED in 434.7s` against 430.5 s between vitest's first and last stamp,
the collection 0.476 s -- `node_tooling_test` 15.3 s, `scripts_lib_test`
1.3 s; critical path 522.25 s, 542.8 s (540.6-561.7). At 801952e the same
cell ran `TsgoDeclare //web:web` to 102 s and `web_test`'s check to 23 s in
the progress lines and `web_test` in 448.9 s (vitest's own `Duration
441.12s`; what lay outside it held the collection over the 48,515-entry
runfiles tree, which vitest now walks over a staged root of the test's 2235
compiled files): 588.6 s (582.3-600.8). At 5049c1b the same cell re-emitted
web through `TsgoDeclare //web:web` at 127 s and ran the test in 474.2 s:
610.7 s (608.7-639.9; 744.3 s at f123f53). A target is the unit of
re-testing, so a one-line change in web runs web's whole suite; the test's
own check no longer waits for its package's declare -- `:web` joins its
program as sources -- but for web's declarations through web-proxy.

**One-line change in a leaf worker, re-test.** The checkout runs vitest in
workers/download: one file, 76 tests, 294 ms, 2.2 s with pnpm's start; CI
typechecks no worker (lint-js.yml generates `worker-configuration.d.ts` for
type-aware oxlint alone, and typecheck.sh's list of 22 tsconfigs at d7346c6
holds none). Bazel runs seven sandboxed actions -- among them `TsCodegen`
for the worker's types and the test itself at 0.8 s -- and hits the action
cache for two: 6.3 s (6.1 s at 801952e, 5.5 s at 5049c1b, 9.2 s at f123f53),
of which the type check is work the checkout's CI never does.

**What a build costs the machine besides itself.** During the cold Bazel
check (346.1 s) the runner's own processes used 2848.5 CPU-seconds, the
kernel's threads 258.0 and the security sensor 551.9; during typecheck.sh
(25.3 s) the kernel 2.4 and the sensor 8.7. The threads copy and encrypt
what Bazel writes; the sensor inspects what it executes and opens. At
801952e the same cells cost 2866.3, 389.4 and 392.0, and 4.7 and 8.8; at
5049c1b 2576.9, 241.8 and 406.9, and 3.3 and 8.5; at f123f53 the cold check
cost 5751.8, 1108.9 and 1401.2: the trees copied per target were most of
what the threads and the sensor saw.
