# Benchmark

Compare equivalent work in the same consumer checkout with `tools/bench_parity.sh`. Report the consumer revision, ruleset revision, exact commands, selected target population, compiler and runtime versions, machine resources, cache state and failures alongside every timing. A benchmark of one consumer does not establish a general speedup.

## Match the work

Use the checkout commands developers and CI actually run. Compare their selected source and test populations with Bazel's targets before measuring. If a failing test is excluded, remove the same population from both sides and identify the exclusion in the benchmark report. A successful build does not establish test parity.

Keep validation enabled when comparing checked builds. `--norun_validations` also disables native type checking. If a comparison deliberately excludes lint, state that scope and select only the no-lint configuration.

Bazel may build toolchains and rule tools during a cold run, while a checkout command starts with installed tools. Separate that setup cost from the consumer build rather than presenting the two as equivalent work.

## Record cache state

Distinguish the first recorded run, a repeat without edits, and a fresh output base using an already populated shared cache. The first recorded run is not necessarily cold. Record action-cache and remote-cache hits, executed actions and whether test-result reuse was enabled. Follow the environment's cache policy; a comparison does not require deleting a shared cache.

A warm `bazel test` result may execute no tests, while the checkout test runner executes them again. Report that difference directly. To compare actual execution, use the same test population and disable Bazel test-result reuse.

## Explain the result

For an edit comparison, apply the same source change to each command's input and report what invalidated. Distinguish checking a single target from checking all dependent programs and rerunning their tests. A source edit can leave emitted declarations byte-identical, allowing downstream actions to reuse cached results.

Use invocation logs and profiles to separate analysis, queueing, dependency installation, program layout, compiler execution and test execution. Record elapsed time and critical-path time separately. Concurrent work, CPU allocation, storage and memory pressure can change the result; retain those observations with the measurements.

Do not infer a compiler or test-runner speedup from a cached result or from two runs with different work. Publish the command, scope and invocation evidence supporting the specific comparison.
