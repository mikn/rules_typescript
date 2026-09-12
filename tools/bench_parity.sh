#!/usr/bin/env bash
set -uo pipefail

usage() {
  echo "usage: tools/bench_parity.sh <checkout> <scratch> <logs> [runs]" >&2
  echo "  BAZEL (default bazelisk); LOCAL_TEST_JOBS (unset: Bazel's own)" >&2
  echo "  EXCLUDE: a file, one '<ts_test label>|<CI row, worker dir or" >&2
  echo "    ->|<why>' per line, left out of the test-everything cells" >&2
  echo "  EXCLUDE_BUILD: the same shape for targets red at the proof's" >&2
  echo "    build, left out of every //... cell on both lanes" >&2
  echo "  OTHER_CORES (default 2): a cell starts once other processes' CPU" >&2
  echo "    is under it over 3 s; a cell during which it averaged more is" >&2
  echo "    redone with its segment, REDO (default 3) attempts before the" >&2
  echo "    runner stops. Kernel threads are not other processes, nor the" >&2
  echo "    /proc/<pid>/comm names in SYSTEM_PROCS (a security sensor" >&2
  echo "    whose CPU follows the benchmark's own activity)" >&2
  exit 2
}
[ $# -ge 3 ] && [ $# -le 4 ] || usage
CHECKOUT="$(cd "$1" && pwd)" || exit 2
SCRATCH="$2"
LOGS="$3"
RUNS="${4:-3}"
BAZEL="${BAZEL:-bazelisk}"
EXCLUDE="${EXCLUDE:-}"
EXCLUDE_BUILD="${EXCLUDE_BUILD:-}"
OTHER_CORES="${OTHER_CORES:-2}"
REDO="${REDO:-3}"
SYSTEM_PROCS="${SYSTEM_PROCS:-}"
TCK="$(getconf CLK_TCK)"
TSGO="--@rules_typescript//ts:declarations=tsgo"
NO_LINT="--@rules_typescript//ts:lint=@rules_typescript//ts:no_lint"
TYPECHECK=.github/scripts/typecheck.sh
TYPECHECK_OUTPUTS='packages/agent-sdk/dist web/shared/i18n/compiled
web/node_modules/.cache/paraglide'
WEB_EDIT=web/shared/lib/markdown/markedRenderer.ts
LEAF=workers/download
LEAF_EDIT=$LEAF/src/index.ts
EDIT_LINE=';'

for f in EXCLUDE EXCLUDE_BUILD; do
  [ -z "${!f}" ] || [ -f "${!f}" ] ||
    { echo "$f=${!f} is not a file" >&2; exit 2; }
done
field() { [ -n "$1" ] && cut -d'|' -f"$2" "$1"; }
EXCL_LABELS="$(field "$EXCLUDE" 1)"
BUILD_LABELS="$(field "$EXCLUDE_BUILD" 1)"
EXCL_ROWS="$({ field "$EXCLUDE" 2; field "$EXCLUDE_BUILD" 2; } | grep -v '^-$')"

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
WARM='warm-check-checkout warm-check-bazel warm-test-checkout warm-test-bazel'
[ -n "$EXCL_LABELS" ] && WARM="$WARM excluded-build"
EDIT='edit-web-check-checkout edit-web-check-bazel edit-web-test-checkout
edit-web-test-bazel edit-leaf-checkout edit-leaf-bazel'
CACHED='cached-check-bazel cached-test-bazel'
ATTEMPT=1

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
load() { cut -d' ' -f1-3 /proc/loadavg; }
avail() { df -BG --output=avail "$SCRATCH" | tail -1 | tr -d ' '; }
memory() {
  awk '/^MemAvailable/{a=$2} /^SwapTotal/{t=$2} /^SwapFree/{f=$2}
    END{printf "mem_avail=%dG swap_used=%dG", a/1048576, (t-f)/1048576}' \
    /proc/meminfo
}
complete() { [ -f "$1" ] && grep -q '^# exit=[0-9]*$' "$1"; }
all_complete() {
  local n="$1" c
  for c in $2; do complete "$LOGS/$c/run$n.log" || return 1; done
}
rm_logs() {
  local n="$1" c
  for c in $2; do rm -f "$LOGS/$c/run$n.log"; done
}
lanes() {
  COB="$SCRATCH/ob-run$1-check" CDC="$SCRATCH/dc-run$1-check"
  TOB="$SCRATCH/ob-run$1-test" TDC="$SCRATCH/dc-run$1-test"
  XOB="$SCRATCH/ob-run$1-cached"
}
lane_caches() { [ -d "$CDC" ] && [ -d "$TDC" ]; }
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

busy_jiffies() { awk 'NR==1{print $2+$3+$4+$7+$8+$9}' /proc/stat; }
proc_jiffies() {
  [ -n "$1" ] && [ -r "/proc/$1/stat" ] || { echo 0; return; }
  cut -d')' -f2 "/proc/$1/stat" | awk '{print $12+$13+$14+$15}'
}
system_snapshot() {
  cat /proc/[0-9]*/stat 2> /dev/null | awk -v names="$SYSTEM_PROCS" '
    BEGIN{n=split(names, a, " "); for (i=1;i<=n;i++) sp[substr(a[i],1,15)]=1}
    {match($0, /\(.*\)/); comm=substr($0, RSTART+1, RLENGTH-2);
     split(substr($0, RSTART+RLENGTH+1), f, " "); c=f[12]+f[13]+f[14]+f[15];
     if ($1 == 2 || f[2] == 2) print $1, c, "k"; else if (comm in sp)
       print $1, c, "s"}' > "$1"
}
system_delta() {
  awk 'NR==FNR{b[$1]=$2; next} {d=$2-($1 in b ? b[$1] : 0); if (d<0) d=0;
    if ($3 == "k") k+=d; else s+=d} END{print k+0, s+0}' "$1" "$2"
}
server_pid() { [ -n "$1" ] && cat "$1/server/server.pid.txt" 2>/dev/null; }
other_cores_now() {
  local a b k g
  system_snapshot "$SCRATCH/cpu.gate0"; a=$(busy_jiffies)
  sleep 3
  b=$(busy_jiffies); system_snapshot "$SCRATCH/cpu.gate1"
  read -r k g <<< "$(system_delta "$SCRATCH/cpu.gate0" "$SCRATCH/cpu.gate1")"
  awk -v a="$((b - a - k - g))" -v t="$TCK" \
    'BEGIN{if (a<0) a=0; printf "%.2f", a/t/3}'
}
over() { awk -v c="$1" -v m="$OTHER_CORES" 'BEGIN{exit !(c >= m)}'; }
wait_quiet() {
  local t0 c
  t0=$(date +%s)
  while c=$(other_cores_now); over "$c"; do
    echo "waiting: other work $c cores over 3 s at $(now)" >&2
    sleep 15
  done
  printf 'other work %s cores over 3 s; waited %ss for under %s' \
    "$c" "$(( $(date +%s) - t0 ))" "$OTHER_CORES"
}
negatives() {
  local l
  for l in $1; do echo "-$l"; done
}
excluded_negatives() { negatives "$EXCL_LABELS"; }
build_universe() { echo '//...'; negatives "$BUILD_LABELS"; }
test_universe() { echo '//...'; excluded_negatives; negatives "$BUILD_LABELS"; }
excluded_row() {
  local r
  for r in $EXCL_ROWS; do [ "$r" = "$1" ] && return 0; done
  return 1
}

run_cell() {
  local name="$1" run="$2" note="$3" cwd="$4" ob="$5"; shift 5
  local log="$LOGS/$name/run$run.log" quiet t0 t1 rc wall cpu cores
  local b0 b1 k g o0 o1 p0 p1 s0 s1
  mkdir -p "$LOGS/$name"
  quiet="$(wait_quiet)"
  {
    header
    printf '# cmd:'; printf ' %q' "$@"
    printf '  (%s run %s: %s)\n' "$name" "$run" "$note"
    printf '# start %s load %s %s; %s\n' "$(now)" "$(load)" "$(memory)" \
      "$quiet"
  } > "$log"
  p0="$(server_pid "$ob")"; s0="$(proc_jiffies "$p0")"
  o0="$(proc_jiffies $$)"; system_snapshot "$SCRATCH/cpu.start"
  b0="$(busy_jiffies)"
  t0=$(date +%s.%N)
  (cd "$cwd" && "$@") >> "$log" 2>&1
  rc=$?
  t1=$(date +%s.%N)
  b1="$(busy_jiffies)"; system_snapshot "$SCRATCH/cpu.end"
  o1="$(proc_jiffies $$)"; p1="$(server_pid "$ob")"; s1="$(proc_jiffies "$p1")"
  [ "$p1" = "$p0" ] || s0=0
  read -r k g <<< "$(system_delta "$SCRATCH/cpu.start" "$SCRATCH/cpu.end")"
  wall="$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}')"
  cpu="$(awk -v b="$((b1 - b0))" -v k="$k" -v g="$g" \
    -v o="$((o1 - o0 + s1 - s0))" -v t="$TCK" -v a="$t0" -v z="$t1" 'BEGIN{
    x=(b-o-k-g)/t; if (x<0) x=0; w=z-a;
    printf "total=%.1f ours=%.1f kernel=%.1f system_procs=%.1f", b/t, o/t,
      k/t, g/t;
    printf " others=%.1f others_cores=%.2f", x, (w>0 ? x/w : 0)}')"
  cores="${cpu##*others_cores=}"
  {
    printf '# wall=%s\n' "$wall"
    printf '# cpu %s\n' "$cpu"
    printf '# end %s load %s scratch_avail=%s\n' "$(now)" "$(load)" "$(avail)"
    printf '# exit=%d\n' "$rc"
  } >> "$log"
  echo "$name run $run: wall $wall s exit $rc; other work $cores cores"
  over "$cores" || return 0
  mkdir -p "$LOGS/$name/contended"
  mv "$log" "$LOGS/$name/contended/run$run-$ATTEMPT.log"
  echo "$name run $run: other work over $OTHER_CORES cores; not a number"
  return 1
}

checkout_cell() {
  local name="$1" run="$2" note="$3" ci="$4" script="$5"
  local file="$SCRATCH/cells/$name.sh"
  mkdir -p "$SCRATCH/cells"
  printf 'set -u\nfail=0\n%s\nexit $fail\n' "$script" > "$file"
  if [ "$ci" = ci ]; then
    run_cell "$name" "$run" "$note; env CI=1" "$CHECKOUT" "" \
      env CI=1 direnv exec "$CHECKOUT" bash "$file"
  else
    run_cell "$name" "$run" "$note" "$CHECKOUT" "" \
      direnv exec "$CHECKOUT" bash "$file"
  fi
}

bazel_cell() {
  local name="$1" run="$2" note="$3" ob="$4" dc="$5" verb="$6"; shift 6
  local -a flags=("$TSGO" "--disk_cache=$dc" "$NO_LINT")
  [ "$verb" = test ] && flags+=(--test_output=errors)
  [ -n "${LOCAL_TEST_JOBS:-}" ] &&
    flags+=("--local_test_jobs=$LOCAL_TEST_JOBS")
  run_cell "$name" "$run" "$note" "$CHECKOUT" "$ob" \
    "$BAZEL" "--output_base=$ob" "$verb" "${flags[@]}" -- "$@"
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
    [ -n "$label" ] || continue
    excluded_row "$label" && continue
    step "$dir" "$label" "$cmd"
  done <<< "$CI_TEST_ROWS"
  for w in $CF_WORKERS; do
    excluded_row "workers/$w" && continue
    step "workers/$w" "cf-workers-test.yml:40" "$CF_TEST"
  done
}
web_check_script() {
  step . "tsc -p web" "node_modules/.bin/tsc -p web --noEmit"
}
web_tests_script() {
  step . "test.yml:1105 --changed" \
    "pnpm --filter=web run test:run --changed"
}
leaf_script() { step "$LEAF" "cf-workers-test.yml:40" "$CF_TEST"; }
clear_vitest_caches() {
  local d n=0
  for d in "$CHECKOUT"/node_modules/.vite/vitest \
    "$CHECKOUT"/*/node_modules/.vite/vitest \
    "$CHECKOUT"/*/*/node_modules/.vite/vitest; do
    [ -d "$d" ] && { rm -rf "$d"; n=$((n + 1)); }
  done
  echo "removed $n vitest cache directories (node_modules/.vite/vitest)"
}
clear_typecheck_outputs() {
  local d n=0
  for d in $TYPECHECK_OUTPUTS; do
    [ -e "$CHECKOUT/$d" ] && git -C "$CHECKOUT" check-ignore -q "$d" &&
      { rm -rf "$CHECKOUT/$d"; n=$((n + 1)); }
  done
  echo "removed $n ignored outputs of $TYPECHECK ($TYPECHECK_OUTPUTS)"
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
trap 'exit 143' TERM INT

shutdown_ob() {
  local -a subtrees
  [ -d "$1" ] || return 0
  (cd "$CHECKOUT" && "$BAZEL" "--output_base=$1" shutdown) > /dev/null 2>&1
  find "$1" -type d ! -perm -u+w -exec chmod u+w {} +
  mapfile -d '' subtrees < <(find "$1" -mindepth 1 -maxdepth 3 -print0)
  printf '%s\0' "${subtrees[@]}" | xargs -0 -r -P 8 -n 32 rm -rf
  rm -rf "$1"
}

segment() {
  local name="$1"; shift
  for ATTEMPT in $(seq 1 "$REDO"); do
    "$@" && return 0
    echo "$name: attempt $ATTEMPT had other work over $OTHER_CORES cores;" \
      "redone"
  done
  echo "$name: other work over $OTHER_CORES cores in $REDO attempts" >&2
  exit 1
}

check_lane() {
  local n="$1" note
  shutdown_ob "$COB"; rm -rf "$CDC"; mkdir -p "$CDC"
  clear_typecheck_outputs
  note="cold: fresh output base, empty disk cache; $TYPECHECK's outputs removed"
  checkout_cell cold-check-checkout "$n" "$note" no "$(typecheck_script)" ||
    return 1
  bazel_cell cold-check-bazel "$n" "$note" "$COB" "$CDC" \
    build $(build_universe) || return 1
  note="warm: the cold row's output base and disk cache, nothing changed"
  checkout_cell warm-check-checkout "$n" "$note" no "$(typecheck_script)" ||
    return 1
  bazel_cell warm-check-bazel "$n" "$note" "$COB" "$CDC" \
    build $(build_universe)
}

test_lane() {
  local n="$1" note
  shutdown_ob "$TOB"; rm -rf "$TDC"; mkdir -p "$TDC"
  clear_vitest_caches
  note="cold: fresh output base, empty disk cache; vitest caches removed"
  checkout_cell cold-test-checkout "$n" "$note" ci "$(tests_script)" ||
    return 1
  bazel_cell cold-test-bazel "$n" "$note" "$TOB" "$TDC" \
    test $(test_universe) || return 1
  note="warm: the cold row's output base and disk cache, nothing changed"
  checkout_cell warm-test-checkout "$n" "$note" ci "$(tests_script)" ||
    return 1
  bazel_cell warm-test-bazel "$n" "$note" "$TOB" "$TDC" \
    test $(test_universe) || return 1
  [ -n "$EXCL_LABELS" ] || return 0
  note="the excluded targets built on the test lane, not a protocol row"
  bazel_cell excluded-build "$n" "$note" "$TOB" "$TDC" build $EXCL_LABELS
}

cache_list() { find "$CDC" "$TDC" -type f | sort; }
cache_forget() {
  cache_list | comm -13 "$SCRATCH/cache-before" - | tr '\n' '\0' |
    xargs -0 -r rm -f
}
unedit() {
  local file="$1" pattern="$2" n="$3" lane
  local note="$file restored; the lane back at the tree before the edit"
  shift 3
  restore "$file"
  for lane in "$@"; do
    if [ "$lane" = check ]; then
      bazel_cell maintenance/unedit-check "$n-$ATTEMPT" "$note" "$COB" "$CDC" \
        build "$pattern" || true
    else
      bazel_cell maintenance/unedit-test "$n-$ATTEMPT" "$note" "$TOB" "$TDC" \
        test "$pattern" $(excluded_negatives) || true
    fi
  done
  cache_forget
}

web_edit() {
  local n="$1" note="warm, '$EDIT_LINE' appended to $WEB_EDIT"
  [ "$ATTEMPT" -eq 1 ] && cache_list > "$SCRATCH/cache-before"
  [ "$ATTEMPT" -gt 1 ] && unedit "$WEB_EDIT" //web/... "$n" check test
  edit "$WEB_EDIT"
  checkout_cell edit-web-check-checkout "$n" "$note" no \
    "$(web_check_script)" || return 1
  bazel_cell edit-web-check-bazel "$n" "$note" "$COB" "$CDC" \
    build //web/... || return 1
  checkout_cell edit-web-test-checkout "$n" "$note" ci \
    "$(web_tests_script)" || return 1
  bazel_cell edit-web-test-bazel "$n" "$note" "$TOB" "$TDC" test //web/...
}

leaf_edit() {
  local n="$1" note="warm, '$EDIT_LINE' appended to $LEAF_EDIT"
  [ "$ATTEMPT" -eq 1 ] && cache_list > "$SCRATCH/cache-before"
  [ "$ATTEMPT" -gt 1 ] && unedit "$LEAF_EDIT" "//$LEAF/..." "$n" test
  edit "$LEAF_EDIT"
  checkout_cell edit-leaf-checkout "$n" "$note" ci "$(leaf_script)" ||
    return 1
  bazel_cell edit-leaf-bazel "$n" "$note" "$TOB" "$TDC" test "//$LEAF/..."
}

cached() {
  local n="$1"
  local note="remote-cache-shaped: fresh output base, the cold row's disk cache"
  shutdown_ob "$XOB-check"; shutdown_ob "$XOB-test"
  bazel_cell cached-check-bazel "$n" "$note" "$XOB-check" "$CDC" \
    build $(build_universe) || return 1
  bazel_cell cached-test-bazel "$n" "$note" "$XOB-test" "$TDC" \
    test $(test_universe)
}

median() { sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}'; }
exclusions_table() {
  local l r w
  [ -n "$1" ] || return 0
  echo
  echo "$2 ($1):"
  echo
  echo "| target | checkout row dropped | why |"
  echo "|---|---|---|"
  while IFS='|' read -r l r w; do
    [ -n "$l" ] && echo "| \`$l\` | $r | $w |"
  done < "$1"
}
summary() {
  local out="$LOGS/summary.md" c logs walls med exits mlog info
  {
    echo "| cell | runs | median s | min s | max s | exits |" \
      "median run's other work (cores) | median run's bazel lines |"
    echo "|---|---|---|---|---|---|---|---|"
    for c in $COLD $WARM $EDIT $CACHED; do
      logs="$(for f in "$LOGS/$c"/run*.log; do
        complete "$f" && echo "$f"; done)"
      [ -n "$logs" ] || { echo "| $c | 0 | | | | | | |"; continue; }
      walls="$(grep -h '^# wall=' $logs | cut -d= -f2)"
      med="$(echo "$walls" | median)"
      exits="$(grep -h '^# exit=[0-9]*$' $logs | cut -d= -f2 | paste -sd,)"
      mlog="$(grep -l "^# wall=$med\$" $logs | head -1)"
      info="$(grep -h -E '^INFO: (Elapsed time|[0-9]+ processes)' "$mlog" |
        sed 's/^INFO: //' | paste -sd';')"
      echo "| $c | $(echo "$logs" | wc -l) | $med |" \
        "$(echo "$walls" | sort -n | head -1) |" \
        "$(echo "$walls" | sort -n | tail -1) | $exits |" \
        "$(sed -n 's/^# cpu .*others_cores=//p' "$mlog") | $info |"
    done
    exclusions_table "$EXCLUDE" "Left out of the test-everything cells"
    exclusions_table "$EXCLUDE_BUILD" 'Left out of every `//...` cell'
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
  "local_test_jobs=${LOCAL_TEST_JOBS:-default} other_cores=$OTHER_CORES" \
  "redo=$REDO system_procs=${SYSTEM_PROCS:-none} exclude=${EXCLUDE:-none}" \
  "exclude_build=${EXCLUDE_BUILD:-none}"
for n in $(seq 1 "$RUNS"); do
  lanes "$n"
  all_complete "$n" "$COLD $WARM $EDIT $CACHED" && continue
  lane_caches || rm_logs "$n" "$COLD $WARM $EDIT"
  if ! all_complete "$n" "$COLD $WARM $EDIT"; then
    rm_logs "$n" "$COLD $WARM $EDIT"
    restore_all
    segment "run $n check lane" check_lane "$n"
    segment "run $n test lane" test_lane "$n"
    segment "run $n web edit" web_edit "$n"
    restore "$WEB_EDIT"
    segment "run $n leaf edit" leaf_edit "$n"
    restore "$LEAF_EDIT"
  fi
  if ! all_complete "$n" "$CACHED"; then
    rm_logs "$n" "$CACHED"
    segment "run $n cached" cached "$n"
  fi
  for d in check test cached-check cached-test; do
    shutdown_ob "$SCRATCH/ob-run$n-$d"
  done
  rm -rf "$CDC" "$TDC"
done
shutdown_ob "$SCRATCH/ob-version"
summary
