#!/usr/bin/env bash
# Exercise the actual release binary before its tag exists. The tiny local
# module proxy gives `go list -m` real versions without fetching the release or
# relying on a replace directive, which would bypass the skew guard.
set -euo pipefail

if [[ $# != 2 ]]; then
  echo "usage: $0 /absolute/path/to/gosx vMAJOR.MINOR.PATCH" >&2
  exit 2
fi
cli=$1
tag=$2
[[ "$cli" = /* && -x "$cli" ]] || { echo "expected an absolute executable path" >&2; exit 2; }
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "expected a stable release tag" >&2; exit 2; }
mismatch=v0.0.0
[[ "$tag" != "$mismatch" ]] || mismatch=v0.0.1

work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/gosx-release-cli.XXXXXX")
proxy="$work/proxy/m31labs.dev/gosx/@v"
app="$work/app"
mkdir -p "$proxy" "$app"
for version in "$tag" "$mismatch"; do
  printf 'module m31labs.dev/gosx\n\ngo 1.26\n' > "$proxy/$version.mod"
  printf '{"Version":"%s","Time":"2026-01-01T00:00:00Z"}\n' "$version" > "$proxy/$version.info"
done
export GOWORK=off GOPROXY="file://$work/proxy" GOSUMDB=off GOPRIVATE=none GONOPROXY=none GONOSUMDB=none
# The synthetic module metadata must never enter the user's real module cache.
export GOMODCACHE="$work/modcache"
unset GOSX_SKIP_VERSION_CHECK
printf 'package app\n\nfunc Page() Node {\n  return <main>release check</main>\n}\n' > "$app/page.gsx"
write_go_mod() {
  printf 'module example.invalid/release-check\n\ngo 1.26\n\nrequire m31labs.dev/gosx %s\n' "$1" > "$app/go.mod"
}

[[ "$("$cli" version)" = "gosx $tag" ]] || { echo "release CLI reports the wrong version" >&2; exit 1; }
write_go_mod "$tag"
[[ "$(cd "$app" && go list -m -f '{{.Version}}' m31labs.dev/gosx)" = "$tag" ]]
"$cli" check "$app/page.gsx"

write_go_mod "$mismatch"
[[ "$(cd "$app" && go list -m -f '{{.Version}}' m31labs.dev/gosx)" = "$mismatch" ]]
if "$cli" check "$app/page.gsx" > "$work/mismatch.log" 2>&1; then
  echo "release CLI accepted a mismatched project version" >&2
  exit 1
fi
if ! grep -Fq "cannot operate on a project pinned to m31labs.dev/gosx $mismatch" "$work/mismatch.log"; then
  cat "$work/mismatch.log" >&2
  echo "release CLI did not report the expected version mismatch" >&2
  exit 1
fi
echo "release CLI $tag accepted its matching project and rejected a mismatched pin; evidence: $work"
