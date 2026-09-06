#!/usr/bin/env bash
# bench_forest.sh — time tsgo over a node_modules forest against the paths map, on one built ts_compile target.
#
# The paths map is what ts_compile writes today: one tsconfig `paths` key per npm package in the target's
# closure, every declaration staged out of its extracted repository. The forest is the M7 shape: a node_modules
# tree laid out as node_modules.bzl lays out a runtime tree (the target's own resolution of each name at the top
# level, every other one under .pnpm/ and linked from the packages that asked for it, each workspace member under
# its link: name with its manifest's source-file targets rewritten to the emitted extensions), `typeRoots` pinned
# to it, `types` as the user's tsconfig chain sets it, and no npm `paths`. Both are staged as execroot replicas
# under the scratch directory and run by hand with the toolchain's tsc, so the timing is the compiler over each
# shape and nothing Bazel adds. bench_forest_plan.py beside this script does the staging and the comparison.
#
# Every workspace member the target depends on is staged and emitted under a forest of its own first, and the
# target's forest reads those declarations: a declaration emitted under the paths map names package-internal
# paths that only a `paths` wildcard resolves.
#
# Per iteration and arm: a check (declaration off, noEmit), the same check with --explainFiles, and the action's
# own declaration emit, interleaved so both arms see the same machine. The walls count only when the two arms'
# diagnostics are identical; the script exits 1 otherwise.
#
# Usage: tools/bench_forest.sh <workspace> <label> <scratch dir> [iterations]
# The target must already be built in <workspace>: its action tsconfig and every dep's declarations have to exist.
# PROBE_PACKAGES, a space-separated list of npm names, adds a per-package report to the comparison.
set -euo pipefail

WS="$1"
LABEL="$2"
SCRATCH="$3"
ITERS="${4:-3}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLAN_PY="$HERE/bench_forest_plan.py"
BAZEL="${BAZEL:-bazel}"

say() { echo "\$ $*"; }
stamp() { echo "# $(date -u +%FT%TZ) load $(cut -d' ' -f1-3 /proc/loadavg)"; }
elapsed() { python3 -c "print(f'{$2-$1:.1f}')"; }
plan_get() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1]));
for k in sys.argv[2].split("."): d=d[k]
print(d)' "$1" "$2"; }
plan_summary() {
  python3 - "$1" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
print("tsc", p["tsc"])
print("forest paths keys kept", p["counts"]["paths_keys_forest"], p["forest_paths_kept"])
print("forest types", p["forest_types"], "from", p["forest_types_source"])
print("action types", p["action_types"], "action files", [f.rsplit("/node_modules/", 1)[-1] for f in p["action_files"]])
print("expected emit outputs", p["expected_outputs"], "action outputs", p["action_outputs"])
print("names with several resolutions", p["counts"]["names_with_several_resolutions"], p["names_with_several_resolutions"][:25])
print("packages beyond the action's inputs", p["packages_beyond_the_inputs"])
print("declared deps the importer resolves differently from the flat hub", json.dumps(p["declared_substitutions"], indent=1))
for m in p["members"]:
    print("member", m["package_name"], m["member_dir"], "files", m["files"], "exports root", m["exports_root"], "nested", m["nested_links"], "declarations from", m["declarations_from"])
PY
}

# run <plan.json> <arm> <variant> <tag> [keep]: one tsc run; emitted .d.ts are counted and deleted unless `keep`.
run() {
  local plan="$1" arm="$2" variant="$3" tag="$4" keep="${5:-}"
  local scratch replica config extra="" out tsc out_dir
  scratch="$(dirname "$plan")"
  tsc="$(plan_get "$plan" tsc)"
  out_dir="$(plan_get "$plan" out_dir)"
  replica="$(plan_get "$plan" "replicas.$arm")"
  case "$variant" in
    check) config="$(plan_get "$plan" "configs.$arm.check")" ;;
    explain) config="$(plan_get "$plan" "configs.$arm.check")"; extra="--explainFiles" ;;
    emit) config="$(plan_get "$plan" "configs.$arm.emit")" ;;
  esac
  out="$scratch/$arm-$variant-$tag.out"
  say "[$arm $variant $tag] $tsc --project $config $extra --extendedDiagnostics --pretty false > $out   (cwd $replica)"
  stamp
  local s e rc=0 wall errors
  s=$(date +%s.%N)
  # shellcheck disable=SC2086
  (cd "$replica" && "$tsc" --project "$config" $extra --extendedDiagnostics --pretty false >"$out" 2>&1) || rc=$?
  e=$(date +%s.%N)
  wall="$(elapsed "$s" "$e")"
  errors="$(grep -c ': error TS' "$out" || true)"
  echo "# tsc exit=$rc wall=${wall}s; $errors error TS lines; $(grep -E '^(Files|Config time|Parse time|Bind time|Check time|Emit time|Total time|Memory used):' "$out" | tr -s ' ' | paste -sd ';')"
  if [[ "$variant" == emit ]]; then
    local written
    written="$(find "$replica/$out_dir" -type f -name '*.d.ts' | wc -l)"
    if [[ -n "$keep" ]]; then
      echo "# emitted .d.ts (regular files under $out_dir): $written; kept for the consumer's forest"
    else
      echo "# emitted .d.ts (regular files under $out_dir): $written; deleted"
      find "$replica/$out_dir" -type f -name '*.d.ts' -delete
    fi
  fi
  WALL["$arm-$variant-$tag"]="$wall"
}

declare -A WALL
mkdir -p "$SCRATCH"
cd "$WS"

say "$BAZEL info execution_root"
EXECROOT="$($BAZEL info execution_root 2>"$SCRATCH/bazel-info.stderr")"
echo "$EXECROOT"
say "$BAZEL aquery 'mnemonic(\"TsgoDeclare\", $LABEL)' --output=jsonproto > $SCRATCH/aquery.jsonproto"
$BAZEL aquery "mnemonic(\"TsgoDeclare\", $LABEL)" --output=jsonproto >"$SCRATCH/aquery.jsonproto" 2>"$SCRATCH/aquery.stderr"
grep -E '^INFO: (Found|Elapsed)' "$SCRATCH/aquery.stderr" || true
say "$BAZEL query $LABEL --output=xml > $SCRATCH/deps.xml"
$BAZEL query "$LABEL" --output=xml >"$SCRATCH/deps.xml" 2>"$SCRATCH/query.stderr"
say "python3 $PLAN_PY members --label $LABEL"
MEMBERS="$(python3 "$PLAN_PY" members --aquery "$SCRATCH/aquery.jsonproto" --deps-xml "$SCRATCH/deps.xml" --execroot "$EXECROOT" --label "$LABEL")"
echo "$MEMBERS"
MEMBER_TARGETS="$(echo "$MEMBERS" | cut -f2 | tr '\n' ' ')"
if [[ -n "${MEMBER_TARGETS// /}" ]]; then
  say "$BAZEL query 'set($MEMBER_TARGETS)' --output=xml > $SCRATCH/members.xml"
  $BAZEL query "set($MEMBER_TARGETS)" --output=xml >"$SCRATCH/members.xml" 2>>"$SCRATCH/query.stderr"
else
  echo '<query version="2"></query>' >"$SCRATCH/members.xml"
fi

# Members first: each is staged under its own pair of replicas, checked on both
# arms, and emitted under its forest; the target's forest reads those files.
MEMBER_GATE=0
MEMBER_VIEWS=()
MEMBER_LABELS=()
while IFS=$'\t' read -r view target; do
  [[ -n "$view" ]] || continue
  MEMBER_VIEWS+=("$view")
  MEMBER_LABELS+=("$target")
done <<<"$MEMBERS"
for ((m = 0; m < ${#MEMBER_VIEWS[@]}; m++)); do
  view="${MEMBER_VIEWS[$m]}"
  target="${MEMBER_LABELS[$m]}"
  dir="$SCRATCH/members/$view"
  mkdir -p "$dir"
  echo
  echo "== member $target ($view)"
  say "$BAZEL aquery 'mnemonic(\"TsgoDeclare\", $target)' --output=jsonproto > $dir/aquery.jsonproto"
  $BAZEL aquery "mnemonic(\"TsgoDeclare\", $target)" --output=jsonproto >"$dir/aquery.jsonproto" 2>"$dir/aquery.stderr"
  say "python3 $PLAN_PY build --label $target --paths $dir/paths --forest $dir/forest --plan $dir/plan.json"
  s=$(date +%s.%N)
  python3 "$PLAN_PY" build --aquery "$dir/aquery.jsonproto" --deps-xml "$SCRATCH/members.xml" --members-xml "$SCRATCH/members.xml" \
    --workspace "$WS" --execroot "$EXECROOT" --label "$target" --paths "$dir/paths" --forest "$dir/forest" --plan "$dir/plan.json"
  e=$(date +%s.%N)
  echo "# staging wall=$(elapsed "$s" "$e")s"
  plan_summary "$dir/plan.json"
  run "$dir/plan.json" forest check 1
  run "$dir/plan.json" paths check 1
  run "$dir/plan.json" forest emit 1 keep
  say "python3 $PLAN_PY compare --plan $dir/plan.json --forest-check $dir/forest-check-1.out --paths-check $dir/paths-check-1.out"
  python3 "$PLAN_PY" compare --plan "$dir/plan.json" --forest-check "$dir/forest-check-1.out" --paths-check "$dir/paths-check-1.out" || MEMBER_GATE=1
done

echo
echo "== $LABEL"
PATHS="$SCRATCH/paths"
FOREST="$SCRATCH/forest"
PLAN="$SCRATCH/plan.json"
say "python3 $PLAN_PY build --label $LABEL --paths $PATHS --forest $FOREST --plan $PLAN --member-forest-root $SCRATCH/members"
stamp
s=$(date +%s.%N)
python3 "$PLAN_PY" build --aquery "$SCRATCH/aquery.jsonproto" --deps-xml "$SCRATCH/deps.xml" --members-xml "$SCRATCH/members.xml" \
  --workspace "$WS" --execroot "$EXECROOT" --label "$LABEL" --paths "$PATHS" --forest "$FOREST" --plan "$PLAN" --member-forest-root "$SCRATCH/members"
e=$(date +%s.%N)
echo "# staging wall=$(elapsed "$s" "$e")s"
say "find $FOREST/node_modules: top-level unscoped names; scoped names; .pnpm entries; symlinks in each replica"
find "$FOREST/node_modules" -mindepth 1 -maxdepth 1 ! -name '@*' ! -name '.pnpm' | wc -l
find "$FOREST/node_modules" -mindepth 2 -maxdepth 2 -path '*/@*' | wc -l
find "$FOREST/node_modules/.pnpm" -mindepth 1 -maxdepth 1 | wc -l
find "$FOREST" -type l | wc -l
find "$PATHS" -type l | wc -l
plan_summary "$PLAN"

for ((i = 1; i <= ITERS; i++)); do
  for variant in check explain emit; do
    for arm in forest paths; do
      run "$PLAN" "$arm" "$variant" "$i"
    done
  done
done

echo
printf '%-5s %-8s %-14s %-14s\n' iter variant forest paths
for ((i = 1; i <= ITERS; i++)); do
  for variant in check explain emit; do
    printf '%-5s %-8s %-14s %-14s\n' "$i" "$variant" "${WALL[forest-$variant-$i]}s" "${WALL[paths-$variant-$i]}s"
  done
done
echo

PROBES=()
for p in ${PROBE_PACKAGES:-}; do PROBES+=(--package "$p"); done
say "python3 $PLAN_PY compare --plan $PLAN --forest-check forest-check-1.out --paths-check paths-check-1.out --forest-explain forest-explain-1.out --paths-explain paths-explain-1.out ${PROBES[*]:-}"
GATE=0
python3 "$PLAN_PY" compare --plan "$PLAN" \
  --forest-check "$SCRATCH/forest-check-1.out" --paths-check "$SCRATCH/paths-check-1.out" \
  --forest-explain "$SCRATCH/forest-explain-1.out" --paths-explain "$SCRATCH/paths-explain-1.out" "${PROBES[@]}" || GATE=1
if [[ $MEMBER_GATE -ne 0 ]]; then
  echo "a member's diagnostics differ between the arms (see its compare above)"
fi
exit $((GATE | MEMBER_GATE))
