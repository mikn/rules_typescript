#!/usr/bin/env bash
set -uo pipefail

usage() {
  echo "usage: tools/bench_parity.sh <checkout> <scratch> <logs> [runs]" >&2
  echo "  BAZEL (default bazelisk); LOCAL_TEST_JOBS (unset: Bazel's own)" >&2
  exit 2
}
[ $# -ge 3 ] && [ $# -le 4 ] || usage
CHECKOUT="$(cd "$1" && pwd)" || exit 2
SCRATCH="$2"
LOGS="$3"
RUNS="${4:-3}"
BAZEL="${BAZEL:-bazelisk}"
TSGO="--@rules_typescript//ts:declarations=tsgo"
TYPECHECK=.github/scripts/typecheck.sh
WEB_EDIT=web/shared/lib/markdown/markedRenderer.ts
LEAF=workers/download
LEAF_EDIT=$LEAF/src/index.ts
EDIT_LINE=';'

CF_WORKERS='web-proxy proxy-worker2 browser-worker entri-webhook
project-redirect-worker o11y-tail-worker dwl-logs-tail-worker
dwl-usage-consumer workflows-dispatch-worker cloud-proxy-tail-worker
api-gateway image-tranformer-worker lovable-web-otel-tail-worker
workflows-otel-tail-worker saml-acs-router dwl-preview-warmer cloud-proxy
download file-viewer document-events mcp-server workflows-compile-worker'
read -r -d '' CF_TEST <<'SH' || true
if jq -e '.scripts."test:coverage"' package.json > /dev/null; then
  pnpm run test:coverage
else
  pnpm run --if-present test -- run
fi
SH
vp=validate-packages.yml
CI_TEST_ROWS="test.yml:1093|.|pnpm --filter=web run paraglide:compile:no-dts
test.yml:1094|.|pnpm --filter @lovablelabs/agent-sdk run test --run
test.yml:1095|.|pnpm --filter @lovable/connector-egress run test --run
test.yml:1096|.|pnpm --filter @lovable/events run test --run
test.yml:1097|.|pnpm --filter @lovable/workflow-sdk run test --run
test.yml:1105|.|pnpm --filter=web run test:run
test.yml:1378|.|pnpm --filter lovable-desktop run test:unit
test-firebase-functions.yml:24|.|pnpm -F firebase/functions test
lint-js.yml:290|.|pnpm --dir script_tag test
$vp:174|.|pnpm --filter @lovable.dev/mcp-js test
$vp:180|.|pnpm --filter @lovable.dev/slides test
$vp:184|.|pnpm --filter @lovable.dev/vite-tanstack-config test
$vp:188|.|pnpm --filter @lovable.dev/cloud-auth-js test
$vp:194|.|pnpm --filter @lovable.dev/sdk test
publish-email-js.yml:50|npm-packages/email-js|pnpm run test"

COLD='cold-check-checkout cold-check-bazel cold-test-checkout cold-test-bazel'
WARM='warm-check-checkout warm-check-bazel warm-test-checkout warm-test-bazel
edit-web-check-checkout edit-web-check-bazel edit-web-test-checkout
edit-web-test-bazel edit-leaf-checkout edit-leaf-bazel'
CACHED='cached-check-bazel cached-test-bazel'

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
load() { cut -d' ' -f1-3 /proc/loadavg; }
complete() { [ -f "$1" ] && grep -q '^# exit=[0-9]*$' "$1"; }
all_complete() {
  local n="$1" c
  for c in $2; do complete "$LOGS/$c/run$n.log" || return 1; done
}
rm_logs() {
  local n="$1" c
  for c in $2; do rm -f "$LOGS/$c/run$n.log"; done
}
lane_caches() {
  [ -d "$SCRATCH/dc-run$1-check" ] && [ -d "$SCRATCH/dc-run$1-test" ]
}
override_sha() {
  local path
  path="$(sed -n '/module_name = "rules_typescript"/,/)/p' \
    "$CHECKOUT/MODULE.bazel" | sed -n 's/.*path = "\(.*\)".*/\1/p')"
  [ -n "$path" ] && git -C "$path" rev-parse HEAD 2>/dev/null || echo none
}
tool_versions() {
  cd "$CHECKOUT" && direnv exec "$CHECKOUT" bash -c \
    'printf "node=%s pnpm=%s tsc=%s" "$(node --version)" "$(pnpm --version)" \
      "$(node_modules/.bin/tsc --version | sed "s/Version //")"' \
    2>/dev/null | tail -1
}
header() {
  printf '# path=%s HEAD=%s tree=%s override=%s bazel=%s %s\n' "$CHECKOUT" \
    "$(git -C "$CHECKOUT" rev-parse HEAD)" \
    "$(git -C "$CHECKOUT" rev-parse 'HEAD^{tree}')" "$OVERRIDE" "$BAZEL_VER" \
    "$TOOLS"
}

run_cell() {
  local name="$1" run="$2" note="$3" cwd="$4"; shift 4
  local log="$LOGS/$name/run$run.log" t0 t1 rc wall
  mkdir -p "$LOGS/$name"
  {
    header
    printf '# cmd:'; printf ' %q' "$@"
    printf '  (%s run %s: %s)\n' "$name" "$run" "$note"
    printf '# start %s load %s\n' "$(now)" "$(load)"
  } > "$log"
  t0=$(date +%s.%N)
  (cd "$cwd" && "$@") >> "$log" 2>&1
  rc=$?
  t1=$(date +%s.%N)
  wall="$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}')"
  {
    printf '# wall=%s\n' "$wall"
    printf '# end %s load %s\n' "$(now)" "$(load)"
    printf '# exit=%d\n' "$rc"
  } >> "$log"
  echo "$name run $run: wall $wall s exit $rc"
}

checkout_cell() {
  local name="$1" run="$2" note="$3" ci="$4" script="$5"
  local file="$SCRATCH/cells/$name.sh"
  mkdir -p "$SCRATCH/cells"
  printf 'set -u\nfail=0\n%s\nexit $fail\n' "$script" > "$file"
  if [ "$ci" = ci ]; then
    run_cell "$name" "$run" "$note; env CI=1" "$CHECKOUT" \
      env CI=1 direnv exec "$CHECKOUT" bash "$file"
  else
    run_cell "$name" "$run" "$note" "$CHECKOUT" \
      direnv exec "$CHECKOUT" bash "$file"
  fi
}

bazel_cell() {
  local name="$1" run="$2" note="$3" ob="$4" dc="$5" verb="$6"; shift 6
  local -a flags=("$TSGO" "--disk_cache=$dc" --norun_validations)
  [ -n "${LOCAL_TEST_JOBS:-}" ] &&
    flags+=("--local_test_jobs=$LOCAL_TEST_JOBS")
  run_cell "$name" "$run" "$note" "$CHECKOUT" \
    "$BAZEL" "--output_base=$ob" "$verb" "${flags[@]}" "$@"
}

step() {
  local dir="$1" label="$2" cmd="$3"
  printf 'echo %q\n' "\$ ($dir) $cmd"
  printf '(cd %q && %s)\nrc=$?\n' "$dir" "$cmd"
  printf 'echo "# step-exit=$rc %s"\n[ $rc -eq 0 ] || fail=1\n' "$label"
}
typecheck_script() { step . "$TYPECHECK" "$TYPECHECK"; }
tests_script() {
  local label dir cmd w
  while IFS='|' read -r label dir cmd; do
    [ -n "$label" ] && step "$dir" "$label" "$cmd"
  done <<< "$CI_TEST_ROWS"
  for w in $CF_WORKERS; do
    step "workers/$w" "cf-workers-test.yml:40" "$CF_TEST"
  done
}
web_check_script() {
  step . "tsc -p web" "node_modules/.bin/tsc -p web --noEmit"
}
web_tests_script() {
  step . "test.yml:1105 --changed" \
    "pnpm --filter=web run test:run -- --changed"
}
leaf_script() {
  step . "tsc -p $LEAF" "node_modules/.bin/tsc -p $LEAF --noEmit"
  step "$LEAF" "cf-workers-test.yml:40" "$CF_TEST"
}
clear_checkout_caches() {
  local d n=0
  for d in "$CHECKOUT"/node_modules/.vite/vitest \
    "$CHECKOUT"/*/node_modules/.vite/vitest \
    "$CHECKOUT"/*/*/node_modules/.vite/vitest; do
    [ -d "$d" ] && { rm -rf "$d"; n=$((n + 1)); }
  done
  echo "removed $n vitest cache directories (node_modules/.vite/vitest)"
}

edit() {
  mkdir -p "$SCRATCH/orig/$(dirname "$1")"
  cp "$CHECKOUT/$1" "$SCRATCH/orig/$1"
  printf '%s\n' "$EDIT_LINE" >> "$CHECKOUT/$1"
}
restore() {
  [ -f "$SCRATCH/orig/$1" ] || return 0
  cp "$SCRATCH/orig/$1" "$CHECKOUT/$1"
  rm "$SCRATCH/orig/$1"
}
restore_all() { restore "$WEB_EDIT"; restore "$LEAF_EDIT"; }
trap restore_all EXIT

shutdown_ob() {
  [ -d "$1" ] || return 0
  (cd "$CHECKOUT" && "$BAZEL" "--output_base=$1" shutdown) > /dev/null 2>&1
  rm -rf "$1"
}

cold_and_warm_rows() {
  local n="$1" note
  local cob="$SCRATCH/ob-run$n-check" cdc="$SCRATCH/dc-run$n-check"
  local tob="$SCRATCH/ob-run$n-test" tdc="$SCRATCH/dc-run$n-test"
  all_complete "$n" "$COLD $WARM" && return
  rm_logs "$n" "$COLD $WARM"
  shutdown_ob "$cob"; shutdown_ob "$tob"
  rm -rf "$cdc" "$tdc"; mkdir -p "$cdc" "$tdc"
  restore_all
  clear_checkout_caches
  note="cold: fresh output base, empty disk cache"
  checkout_cell cold-check-checkout "$n" "$note; vitest caches removed" no \
    "$(typecheck_script)"
  bazel_cell cold-check-bazel "$n" "$note" "$cob" "$cdc" build //...
  checkout_cell cold-test-checkout "$n" "$note" ci "$(tests_script)"
  bazel_cell cold-test-bazel "$n" "$note" "$tob" "$tdc" test //...
  note="warm: the cold row's output base and disk cache, nothing changed"
  checkout_cell warm-check-checkout "$n" "$note" no "$(typecheck_script)"
  bazel_cell warm-check-bazel "$n" "$note" "$cob" "$cdc" build //...
  checkout_cell warm-test-checkout "$n" "$note" ci "$(tests_script)"
  bazel_cell warm-test-bazel "$n" "$note" "$tob" "$tdc" test //...
  note="warm, '$EDIT_LINE' appended to $WEB_EDIT"
  edit "$WEB_EDIT"
  checkout_cell edit-web-check-checkout "$n" "$note" no "$(web_check_script)"
  bazel_cell edit-web-check-bazel "$n" "$note" "$cob" "$cdc" build //web/...
  checkout_cell edit-web-test-checkout "$n" "$note" ci "$(web_tests_script)"
  bazel_cell edit-web-test-bazel "$n" "$note" "$tob" "$tdc" test //web/...
  restore "$WEB_EDIT"
  note="warm, '$EDIT_LINE' appended to $LEAF_EDIT"
  edit "$LEAF_EDIT"
  checkout_cell edit-leaf-checkout "$n" "$note" ci "$(leaf_script)"
  bazel_cell edit-leaf-bazel "$n" "$note" "$tob" "$tdc" test "//$LEAF/..."
  restore "$LEAF_EDIT"
}

cached_row() {
  local n="$1" ob="$SCRATCH/ob-run$n-cached" dc="$SCRATCH/dc-run$n"
  local note="remote-cache-shaped: fresh output base, the cold row's disk cache"
  all_complete "$n" "$CACHED" && return
  rm_logs "$n" "$CACHED"
  shutdown_ob "$ob-check"; shutdown_ob "$ob-test"
  bazel_cell cached-check-bazel "$n" "$note" "$ob-check" "$dc-check" build //...
  bazel_cell cached-test-bazel "$n" "$note" "$ob-test" "$dc-test" test //...
}

median() { sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}'; }
summary() {
  local out="$LOGS/summary.md" c logs walls med exits mlog info
  {
    echo "| cell | runs | median s | min s | max s | exits |" \
      "median run's bazel lines |"
    echo "|---|---|---|---|---|---|---|"
    for c in $COLD $WARM $CACHED; do
      logs="$(for f in "$LOGS/$c"/run*.log; do
        complete "$f" && echo "$f"; done)"
      [ -n "$logs" ] || { echo "| $c | 0 | | | | | |"; continue; }
      walls="$(grep -h '^# wall=' $logs | cut -d= -f2)"
      med="$(echo "$walls" | median)"
      exits="$(grep -h '^# exit=[0-9]*$' $logs | cut -d= -f2 | paste -sd,)"
      mlog="$(grep -l "^# wall=$med\$" $logs | head -1)"
      info="$(grep -h -E '^INFO: (Elapsed time|[0-9]+ processes)' "$mlog" |
        sed 's/^INFO: //' | paste -sd';')"
      echo "| $c | $(echo "$logs" | wc -l) | $med |" \
        "$(echo "$walls" | sort -n | head -1) |" \
        "$(echo "$walls" | sort -n | tail -1) | $exits | $info |"
    done
  } > "$out"
  cat "$out"
}

[ -z "$(git -C "$CHECKOUT" status --short)" ] || {
  echo "$CHECKOUT is not clean (git status --short); refusing to edit it" >&2
  exit 1
}
mkdir -p "$SCRATCH" "$LOGS"
OVERRIDE="$(override_sha)"
BAZEL_VER="$(cd "$CHECKOUT" &&
  "$BAZEL" "--output_base=$SCRATCH/ob-version" version 2>/dev/null |
  sed -n 's/^Build label: //p')"
TOOLS="$(tool_versions)"
echo "# $(now) load $(load) runs=$RUNS" \
  "local_test_jobs=${LOCAL_TEST_JOBS:-default}"
for n in $(seq 1 "$RUNS"); do
  all_complete "$n" "$COLD $WARM $CACHED" && continue
  lane_caches "$n" || rm_logs "$n" "$COLD $WARM"
  cold_and_warm_rows "$n"
  cached_row "$n"
  for d in check test cached-check cached-test; do
    shutdown_ob "$SCRATCH/ob-run$n-$d"
  done
  rm -rf "$SCRATCH/dc-run$n-check" "$SCRATCH/dc-run$n-test"
done
shutdown_ob "$SCRATCH/ob-version"
summary
