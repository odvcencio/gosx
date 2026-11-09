#!/usr/bin/env bash
# Decide whether a failing corpus check is covered by known.tsv.
# usage: waived.sh KNOWN_TSV CONSUMER CHECK [LOGFILE]
# Exit 0 when the failure is waived, 1 when it is not.
#
# known.tsv columns: consumer, check, reason, tests (optional).
#   - tests empty: the whole check is waived (any failure of that check).
#   - tests set: comma-separated "package:TestName" entries, the package as
#     `go test` prints it (for example gridiron-2000/app/admin:TestFoo). The
#     check is waived only if the log shows at least one failing test, every
#     failing top-level test is listed, and every FAIL package line has a named
#     failing test (a build failure, panic or timeout is not waived).
set -uo pipefail
known="${1:?known.tsv}"; consumer="${2:?consumer}"; check="${3:?check}"; log="${4:-}"

row="$(awk -F'\t' -v c="$consumer" -v k="$check" '$0 !~ /^#/ && $1==c && $2==k { print; exit }' "$known")"
[ -n "$row" ] || exit 1
tests="$(printf '%s' "$row" | awk -F'\t' '{ print $4 }')"
[ -n "$tests" ] || exit 0
[ -n "$log" ] && [ -f "$log" ] || exit 1

awk -v tests="$tests" '
  BEGIN { n = split(tests, t, ","); for (i = 1; i <= n; i++) allow[t[i]] = 1 }
  /^--- FAIL: / { name = $3; pend[++np] = name; next }
  /^FAIL\t/ && $0 !~ /^FAIL$/ {
    pkg = $2
    if (np == 0) { bad = 1 }             # package failed with no named test
    for (i = 1; i <= np; i++) { seen++; if (!((pkg ":" pend[i]) in allow)) bad = 1 }
    np = 0; next
  }
  END { exit (bad || seen == 0 || np > 0) ? 1 : 0 }
' "$log"
