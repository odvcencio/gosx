#!/usr/bin/env bash
# GoSX consumer corpus runner. See scripts/corpus/README.md.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
MANIFEST="$HERE/manifest.tsv"
ROOT="${CORPUS_ROOT:-/home/draco/work/.runtime/gosx-corpus}"
LOCK="${CORPUS_LOCK:-/home/draco/work/.runtime/locks/gosx-heavy.lock}"
REMOTE=""; GOSX="$REPO"; GOSX_REF=""; ONLY=""; BASELINE=0; KEEP=0; APPEND=0
BUDGET="${CORPUS_BUDGET:-3600}"      # seconds of check run time; lock waits excluded
STEP_TMO="${CORPUS_STEP_TIMEOUT:-10m}" # build, vet, resolve, wasm

usage() {
  cat <<U
usage: run.sh [-g GOSX_DIR] [-r GIT_REF] [-c name,name] [-o OUT_ROOT] [-b] [-k] [-B SECONDS] [-m MANIFEST]
  -g DIR   GoSX checkout to test (default: the repo holding this script)
  -r REF   instead export REF of that repo into a clean dir (e.g. origin/main)
  -c LIST  only these consumers (comma separated)
  -b       rerun each failed check against the consumer's own pinned GoSX ("baseline")
  -a       append: keep the existing matrix and replace only the -c consumers
  -k       keep the consumer copies under \$CORPUS_ROOT/work
  -R HOST  run each check on a tailnet host (tailnet-run --host HOST), e.g. buildbox; no local lock
  -B SEC   total time budget (default $BUDGET)
U
  exit 2
}
while getopts "g:r:c:o:bkaB:m:R:h" o; do
  case $o in
    g) GOSX="$OPTARG";; r) GOSX_REF="$OPTARG";; c) ONLY="$OPTARG";; o) ROOT="$OPTARG";;
    b) BASELINE=1;; a) APPEND=1;; k) KEEP=1;; B) BUDGET="$OPTARG";; R) REMOTE="$OPTARG";; m) MANIFEST="$OPTARG";; *) usage;;
  esac
done

[ -d "$(dirname "$LOCK")" ] || LOCK="$ROOT/.lock"  # hosted CI has no shared heavy lock
export TMPDIR="${TMPDIR:-/home/draco/work/.runtime/tmp}"
mkdir -p "$TMPDIR" "$ROOT/work"
export GOWORK=off GOMEMLIMIT="${GOMEMLIMIT:-1500MiB}" GOFLAGS="-p=2 -mod=mod"
START=$SECONDS

if [ -n "$GOSX_REF" ]; then
  git -C "$REPO" fetch -q origin 2>/dev/null || echo "warn: fetch origin failed; using local ref" >&2
  SHA="$(git -C "$REPO" rev-parse --short=12 "$GOSX_REF")" || exit 1
  GOSX="$ROOT/work/gosx-$SHA"
  rm -rf "$GOSX"; mkdir -p "$GOSX"
  git -C "$REPO" archive "$GOSX_REF" | tar -x -C "$GOSX"
else
  GOSX="$(cd "$GOSX" && pwd)"
  SHA="$(git -C "$GOSX" rev-parse --short=12 HEAD)"
  [ -n "$(git -C "$GOSX" status --porcelain 2>/dev/null)" ] && SHA="$SHA-dirty"
fi
GOSX_VER="$(grep -m1 -E 'Current *=' "$GOSX/internal/version/version.go" | grep -oE 'v[0-9][^"]*')"
OUT="$ROOT/$SHA"
if [ "$APPEND" = 1 ] && [ -f "$OUT/matrix.tsv" ]; then
  for n in ${ONLY//,/ }; do
    awk -F'\t' -v n="$n" '$1!=n' "$OUT/matrix.tsv" >"$OUT/matrix.tsv.new" && mv "$OUT/matrix.tsv.new" "$OUT/matrix.tsv"
    awk -F'\t' -v n="$n" '$1!=n' "$OUT/consumers.tsv" >"$OUT/consumers.tsv.new" && mv "$OUT/consumers.tsv.new" "$OUT/consumers.tsv"
  done
else
  rm -rf "$OUT"; mkdir -p "$OUT/logs"
  printf 'consumer\tcheck\tstatus\tbaseline\tseconds\tfirst_error\tlog\n' >"$OUT/matrix.tsv"
  printf 'consumer\tref\tcommit\tpinned_gosx\tpublic\n' >"$OUT/consumers.tsv"
fi
TSV="$OUT/matrix.tsv"

first_error() { # logfile -> one line
  local l
  l="$(grep -m1 -E '(\.go:[0-9]+(:[0-9]+)?: )|^go: |^FAIL|^--- FAIL|^panic:|cannot find|no required module|^(fatal|error):' "$1" 2>/dev/null)"
  [ -z "$l" ] && l="$(grep -v '^[[:space:]]*$' "$1" 2>/dev/null | tail -n1)"
  printf '%s' "$l" | tr '\t|' '  ' | cut -c1-220
}

# run_step LOG TIMEOUT cmd...   -> sets STATUS, SECS. The caller holds the heavy lock (fd 9).
run_step() {
  local log="$1" tmo="$2"; shift 2
  local t0; t0="$(date +%s)"
  # A remote step may first wait for a free slot; allow for that on top of the step timeout.
  [ -n "$REMOTE" ] && tmo="$(( ${tmo%m} + ${CORPUS_SLOT_WAIT:-30} ))m"
  timeout -k 20 "$tmo" "$@" >"$log" 2>&1
  local rc=$?
  SECS=$(( $(date +%s) - t0 ))
  ACTIVE=$((ACTIVE + SECS))
  case $rc in 0) STATUS=PASS;; 124|137) STATUS=TIMEOUT;; *) STATUS=FAIL;; esac
}

prepare_replace() { # run in module dir
  local p sub dir
  for p in $(go mod edit -json | jq -r '.Require[]?.Path | select(. == "m31labs.dev/gosx" or startswith("m31labs.dev/gosx/"))'); do
    sub="${p#m31labs.dev/gosx}"; sub="${sub#/}"
    dir="$GOSXDIR${sub:+/$sub}"
    [ -f "$dir/go.mod" ] || continue
    for v in $(go mod edit -json | jq -r --arg p "$p" '.Replace[]? | select(.Old.Path==$p) | (.Old.Version // "")'); do
      if [ -n "$v" ]; then go mod edit -dropreplace="$p@$v"; else go mod edit -dropreplace="$p"; fi
    done
    go mod edit -replace "$p=$dir"
  done
}

record() { # consumer check status baseline secs log
  local err=""
  [ "$3" != PASS ] && [ "$3" != SKIP ] && [ "$3" != N/A ] && err="$(first_error "$OUT/logs/$6")"
  [ "$3" = SKIP ] && err="$7"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" "$err" "logs/$6" >>"$TSV"
}

ACTIVE=0 # seconds spent running checks; lock waits are not counted
over_budget() { [ "$ACTIVE" -ge "$BUDGET" ]; }

while IFS=$'\t' read -r -u 3 NAME URL REF MODDIR BUILD VET TEST TTMO WASM PUBLIC; do
  case "$NAME" in ''|\#*) continue;; esac
  if [ -n "$ONLY" ] && ! printf ',%s,' "$ONLY" | grep -q ",$NAME,"; then continue; fi
  echo "== $NAME" >&2
  WD="$ROOT/work/$NAME"
  rm -rf "$WD"; mkdir -p "$WD"
  if ! ( cd "$WD" && git init -q && git remote add origin "$URL" && git fetch -q --depth 1 origin "$REF" && git checkout -q FETCH_HEAD ) >"$OUT/logs/$NAME-fetch.log" 2>&1; then
    for c in resolve build vet test wasm; do record "$NAME" "$c" SKIP - 0 "$NAME-fetch.log" "clone/fetch failed: $(first_error "$OUT/logs/$NAME-fetch.log")"; done
    continue
  fi
  COMMIT="$(git -C "$WD" rev-parse --short=12 HEAD)"
  MD="$WD/$MODDIR"
  PINNED="$(cd "$MD" && go mod edit -json | jq -r '[.Require[]? | select(.Path=="m31labs.dev/gosx") | .Version][0] // "-"')"
  REPL="$(cd "$MD" && go mod edit -json | jq -r '[.Replace[]? | select(.Old.Path=="m31labs.dev/gosx") | .New.Path + " " + (.New.Version // "")][0] // empty')"
  [ -n "$REPL" ] && PINNED="$PINNED (replaced by $REPL)"
  printf '%s\t%s\t%s\t%s\t%s\n' "$NAME" "$REF" "$COMMIT" "$PINNED" "$PUBLIC" >>"$OUT/consumers.tsv"
  cp "$MD/go.mod" "$MD/go.mod.corpus-orig"; cp "$MD/go.sum" "$MD/go.sum.corpus-orig" 2>/dev/null || : >"$MD/go.sum.corpus-orig"
  GOSXDIR="$GOSX"
  if [ -n "$REMOTE" ]; then # the host cannot see our paths: ship GoSX inside the copy
    mkdir -p "$WD/_corpus-gosx"; cp -a "$GOSX/." "$WD/_corpus-gosx/"; rm -rf "$WD/_corpus-gosx/.git"
    GOSXDIR="$(realpath --relative-to="$MD" "$WD/_corpus-gosx")"; case "$GOSXDIR" in ./*|../*) ;; *) GOSXDIR="./$GOSXDIR";; esac
  fi
  if ! ( cd "$MD" && set -e && prepare_replace && go mod edit -json | jq -e '[.Replace[]? | select(.Old.Path=="m31labs.dev/gosx")] | length == 1' >/dev/null ) >>"$OUT/logs/$NAME-fetch.log" 2>&1; then
    for c in resolve build vet test wasm; do record "$NAME" "$c" SKIP - 0 "$NAME-fetch.log" "could not point m31labs.dev/gosx at the GoSX checkout"; done
    rm -rf "$WD"; continue
  fi

  # check name | timeout | env | command
  declare -a CHECKS=()
  CHECKS+=("resolve|$STEP_TMO||go list -mod=mod -m m31labs.dev/gosx | grep '=>' && go list -mod=mod -deps -f '' $BUILD")
  CHECKS+=("build|$STEP_TMO||go build -mod=mod $BUILD")
  CHECKS+=("vet|$STEP_TMO||go vet -mod=mod $VET")
  CHECKS+=("test|$TTMO|CGO_ENABLED=0|go test -mod=mod -short -count=1 -timeout $TTMO $TEST")
  if [ "$WASM" = "-" ]; then CHECKS+=("wasm|0||")
  else CHECKS+=("wasm|$STEP_TMO|GOOS=js GOARCH=wasm|go build -mod=mod -o /dev/null $WASM"); fi

  # Hold the heavy lock for this consumer's checks, one consumer at a time.
  if [ -n "$REMOTE" ]; then
    WRAP=(/home/draco/.local/bin/tailnet-run --host "$REMOTE" "$WD" bash -c); RUNDIR="$MODDIR"
    ENVX="GOFLAGS=-mod=mod GOMEMLIMIT=1500MiB"
  else
    WRAP=(bash -c); RUNDIR="$MD"; ENVX=""
    exec 9>"$LOCK"; flock 9
  fi
  RESOLVE_OK=1
  for spec in "${CHECKS[@]}"; do
    IFS='|' read -r CK TMO ENVV CMD <<<"$spec"
    LOG="$NAME-$CK.log"
    if [ -z "$CMD" ]; then record "$NAME" "$CK" N/A - 0 "$LOG" ""; continue; fi
    if [ "$RESOLVE_OK" = 0 ]; then record "$NAME" "$CK" SKIP - 0 "$LOG" "resolve failed"; continue; fi
    if over_budget; then record "$NAME" "$CK" SKIP - 0 "$LOG" "time budget ${BUDGET}s spent"; continue; fi
    STATUS=; SECS=0
    run_step "$OUT/logs/$LOG" "$TMO" "${WRAP[@]}" "cd '$RUNDIR' && env $ENVX $ENVV $CMD" </dev/null
    BASE=-
    if [ "$STATUS" != PASS ] && [ "$BASELINE" = 1 ] && ! over_budget; then
      cp "$MD/go.mod" "$MD/go.mod.corpus-new"; cp "$MD/go.sum" "$MD/go.sum.corpus-new" 2>/dev/null
      cp "$MD/go.mod.corpus-orig" "$MD/go.mod"; cp "$MD/go.sum.corpus-orig" "$MD/go.sum"
      NEWSTATUS="$STATUS"; NEWSECS="$SECS"
      BCMD="${CMD//-mod=mod /}"
      run_step "$OUT/logs/$NAME-$CK.baseline.log" "$TMO" "${WRAP[@]}" "cd '$RUNDIR' && env $ENVX $ENVV $BCMD" </dev/null
      BASE="$STATUS"; STATUS="$NEWSTATUS"; SECS="$NEWSECS"
      cp "$MD/go.mod.corpus-new" "$MD/go.mod"; cp "$MD/go.sum.corpus-new" "$MD/go.sum" 2>/dev/null
    fi
    record "$NAME" "$CK" "$STATUS" "$BASE" "$SECS" "$LOG" ""
    [ "$CK" = resolve ] && [ "$STATUS" != PASS ] && RESOLVE_OK=0
  done
  [ -n "$REMOTE" ] || { flock -u 9; exec 9>&-; }
  unset CHECKS
  [ "$KEEP" = 1 ] || rm -rf "$WD"
done 3<"$MANIFEST"

[ -n "$GOSX_REF" ] && [ "$KEEP" != 1 ] && rm -rf "$GOSX"

# Markdown matrix
MD="$OUT/matrix.md"
{
  echo "# GoSX consumer corpus: $SHA"
  echo
  echo "- GoSX: \`$SHA\`${GOSX_REF:+ (from \`$GOSX_REF\`)}, version $GOSX_VER"
  echo "- Go: $(go version | cut -d' ' -f3), date: $(date -u +%Y-%m-%dT%H:%MZ), wall time: $((SECONDS - START))s, check run time: ${ACTIVE}s (budget ${BUDGET}s, lock waits excluded)"
  echo "- Cell format: STATUS, or STATUS (base STATUS) when run with -b against the consumer's own pinned GoSX."
  echo
  echo "| consumer | commit | pinned GoSX | resolve | build | vet | test | wasm |"
  echo "|---|---|---|---|---|---|---|---|"
  awk -F'\t' '
    FNR==NR { if (FNR>1) { c[$1]=$3; p[$1]=$4; order[++n]=$1 } next }
    FNR>1 { cell=$3; if ($4!="-") cell=cell " (base " $4 ")"; m[$1,$2]=cell }
    END { for (i=1;i<=n;i++) { k=order[i]; printf "| %s | %s | %s |", k, c[k], p[k];
          split("resolve build vet test wasm", ck, " "); for (j=1;j<=5;j++) printf " %s |", ((k,ck[j]) in m)? m[k,ck[j]] : "-"; print "" } }
  ' "$OUT/consumers.tsv" "$TSV"
  echo
  echo "## First error per non-passing check"
  echo
  echo "| consumer | check | status | seconds | first error |"
  echo "|---|---|---|---|---|"
  awk -F'\t' 'NR>1 && $3!="PASS" && $3!="N/A" { printf "| %s | %s | %s | %s | `%s` |\n", $1,$2,$3,$5,$6 }' "$TSV"
  echo
  echo "Logs: \`logs/<consumer>-<check>.log\` next to this file."
} >"$MD"
echo "matrix: $MD" >&2
cat "$MD"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then cat "$MD" >>"$GITHUB_STEP_SUMMARY"; fi

# Exit 1 when a check fails and (with -b) its baseline passed, or (without -b) it failed at all.
awk -F'\t' -v b="$BASELINE" 'NR>1 && ($3=="FAIL"||$3=="TIMEOUT") && (b==0 || $4=="PASS") { bad=1 } END { exit bad }' "$TSV" || exit 1
