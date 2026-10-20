#!/usr/bin/env bash
# lh.sh replacement for showcase-receipts.sh: Lighthouse in WSL, headless
# Windows Chrome over DevTools. Same interface and Lighthouse settings as the
# Linux helper: lh.sh <base-url> <out-dir> <runs> <path...>. Chrome restarts
# with a fresh profile before every run, so each run is cold.
#
# Needs: GOSX_RECEIPT_LH_NODE_DIR (directory with node_modules/lighthouse and
# chrome-launcher), GOSX_RECEIPT_LH_CHROME_PORT and GOSX_RECEIPT_LH_BRIDGE_PORT
# (the relay to that Chrome must already run).
set -uo pipefail
script_dir=$(cd "$(dirname "$(readlink -f "$0")")" && pwd)
# shellcheck source=showcase-windows-cdp.sh
source "$script_dir/showcase-windows-cdp.sh"

node_dir=${GOSX_RECEIPT_LH_NODE_DIR:?set GOSX_RECEIPT_LH_NODE_DIR}
chrome_port=${GOSX_RECEIPT_LH_CHROME_PORT:?set GOSX_RECEIPT_LH_CHROME_PORT}
bridge_port=${GOSX_RECEIPT_LH_BRIDGE_PORT:?set GOSX_RECEIPT_LH_BRIDGE_PORT}
base=$1; out=$2; runs=$3; shift 3
mkdir -p "$out"
host_ip=$(wcdp_host_ip)

# Lighthouse's own Chrome launcher flags, so the browser matches what
# lighthouse would start on Linux (chrome-launcher DEFAULT_FLAGS).
flags_file=$(mktemp)
(cd "$node_dir" && node --input-type=module -e "
import { Launcher } from 'chrome-launcher';
console.log(Launcher.defaultFlags().filter(f => f !== '--disable-gpu').join('\n'));
") > "$flags_file"

chrome_pid=
failed=0
cleanup() {
  wcdp_stop_pid "$chrome_pid"
  rm -f "$flags_file"
}
trap cleanup EXIT

for p in "$@"; do
  slug=$(echo "$p" | sed -e 's#^/##' -e 's#/#_#g'); [ -z "$slug" ] && slug=home
  for i in $(seq 1 "$runs"); do
    profile="lh-$slug-$i"
    # A failed run must not leave an earlier run's result for the receipt.
    rm -f "$out/$slug-run$i.json" "$out/$slug-run$i.err"
    # A broken native transport must not leave us measuring the preceding
    # browser/profile and calling that run cold.
    if ! chrome_pid=$(wcdp_start_chrome "$chrome_port" "$profile" "$flags_file") ||
      [[ ! "$chrome_pid" =~ ^[1-9][0-9]*$ ]]; then
      echo "$p run$i FAILED owned Chrome did not start" >&2
      exit 1
    fi
    if ! wcdp_wait_port "http://$host_ip:$bridge_port"; then
      echo "$p run$i FAILED Chrome did not start" >&2
      exit 1
    fi
    if ! (cd "$node_dir" && nice -n 10 timeout 180 ./node_modules/.bin/lighthouse "$base$p" --quiet \
      --hostname="$host_ip" --port="$bridge_port" \
      --only-categories=performance,accessibility,best-practices,seo \
      --output=json --output-path="$out/$slug-run$i.json" >/dev/null 2>"$out/$slug-run$i.err"); then
      rm -f "$out/$slug-run$i.json"
      failed=1
    fi
    if ! wcdp_stop_pid_checked "$chrome_pid"; then
      echo "$p run$i FAILED owned Chrome cleanup could not be verified" >&2
      exit 1
    fi
    chrome_pid=
    wcdp_remove_profile "$profile"
    node -e "const r=require('$out/$slug-run$i.json');const c=r.categories,a=r.audits;console.log('$p run$i','perf',Math.round(c.performance.score*100),'a11y',Math.round(c.accessibility.score*100),'bp',Math.round(c['best-practices'].score*100),'seo',Math.round(c.seo.score*100),'LCP',Math.round(a['largest-contentful-paint'].numericValue),'CLS',a['cumulative-layout-shift'].numericValue.toFixed(3),'TBT',Math.round(a['total-blocking-time'].numericValue),'FCP',Math.round(a['first-contentful-paint'].numericValue),'bytes',Math.round(a['total-byte-weight'].numericValue/1024)+'KB')" 2>/dev/null || { echo "$p run$i FAILED $(tail -2 "$out/$slug-run$i.err" 2>/dev/null)"; failed=1; }
  done
done
exit "$failed"
