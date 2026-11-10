#!/usr/bin/env bash
# Self-test for waived.sh. Run: scripts/corpus/waived_test.sh
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
D="$(mktemp -d)"; trap 'rm -rf "$D"' EXIT
fails=0
expect() { # want name consumer check log
  local want="$1" name="$2"; shift 2
  "$HERE/waived.sh" "$D/known.tsv" "$@"; local got=$?
  if [ "$got" = "$want" ]; then echo "ok   $name"; else echo "FAIL $name: exit $got, want $want"; fails=$((fails+1)); fi
}
printf '# header\nwhole\ttest\twhole-check waiver\nwhole\tvet\tother\nnamed\ttest\treason\tapp/x:TestA,app/x:TestB\n' >"$D/known.tsv"
printf -- '--- FAIL: TestA (0.1s)\nFAIL\nFAIL\tapp/x\t0.2s\nok  \tapp/y\t0.1s\n' >"$D/a.log"
printf -- '--- FAIL: TestA (0.1s)\n--- FAIL: TestB (0.1s)\nFAIL\nFAIL\tapp/x\t0.2s\n' >"$D/ab.log"
printf -- '--- FAIL: TestA (0.1s)\n--- FAIL: TestC (0.1s)\nFAIL\nFAIL\tapp/x\t0.2s\n' >"$D/ac.log"
printf -- '--- FAIL: TestA (0.1s)\nFAIL\nFAIL\tapp/z\t0.2s\n' >"$D/otherpkg.log"
printf -- '--- FAIL: TestA (0.1s)\nFAIL\nFAIL\tapp/x\t0.2s\nFAIL\tapp/y [build failed]\n' >"$D/build.log"
printf 'panic: boom\nFAIL\tapp/x\t0.2s\n' >"$D/panic.log"
printf 'something odd\n' >"$D/none.log"
expect 0 "whole-check waiver needs no log"        whole test "$D/ac.log"
expect 1 "whole-check waiver is per check"        whole build "$D/ac.log"
expect 1 "unknown consumer"                       nobody test "$D/a.log"
expect 0 "named test listed"                      named test "$D/a.log"
expect 0 "two named tests listed"                 named test "$D/ab.log"
expect 1 "an unlisted test also fails"            named test "$D/ac.log"
expect 1 "same test name in another package"      named test "$D/otherpkg.log"
expect 1 "extra package fails to build"           named test "$D/build.log"
expect 1 "panic with no named test"               named test "$D/panic.log"
expect 1 "log without failures"                   named test "$D/none.log"
expect 1 "missing log"                            named test "$D/missing.log"
exit $((fails > 0))
