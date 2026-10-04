#!/usr/bin/env sh
set -eu
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
repo_root="$(dirname "$script_dir")"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
mkdir -p "$tmp_dir/tools" "$tmp_dir/perf/wire/testdata/counter"
cp "$repo_root"/perf/wire/testdata/counter/page.* "$tmp_dir/perf/wire/testdata/counter/"
export FAKE_TOOLS="$tmp_dir/tools" FAKE_LISTEN_LOG="$tmp_dir/listen.log"
cat > "$FAKE_TOOLS/app" <<'APP'
#!/usr/bin/env sh
set -eu
if [ "${GOSX_LISTEN_ADDR:-}" != "$PORT" ]; then
 echo "local launch inherited a conflicting listen address" >&2
 exit 96
fi
case "$GOSX_LISTEN_ADDR" in 127.0.0.1:*) ;; *) exit 97 ;; esac
printf '%s\n' "$GOSX_LISTEN_ADDR" >> "$FAKE_LISTEN_LOG"
trap 'exit 0' TERM INT
while :; do sleep 0.1; done
APP
cat > "$FAKE_TOOLS/gosx" <<'GOSX'
#!/usr/bin/env sh
set -eu
case "$1" in
 init) mkdir -p "$2/app" ;;
 build)
  for app_dir; do :; done
  mkdir -p "$app_dir/dist/server"
  cp "$FAKE_TOOLS/app" "$app_dir/dist/run.sh"
  cp "$FAKE_TOOLS/app" "$app_dir/dist/server/app"
  printf '{}\n' > "$app_dir/dist/build.json"
  ;;
 *) exit 98 ;;
esac
GOSX
cat > "$FAKE_TOOLS/go" <<'GO'
#!/usr/bin/env sh
set -eu
case "$1" in
 build)
  shift
  test "$1" = -o
  case "$3" in
   ./cmd/gosx) cp "$FAKE_TOOLS/gosx" "$2" ;;
   ./perf/wire/cmd/wiregate) cp "$FAKE_TOOLS/wiregate" "$2" ;;
   *) exit 98 ;;
  esac
  ;;
 run) shift 2; exec "$FAKE_TOOLS/gosx" "$@" ;;
 *) exit 98 ;;
esac
GO
cat > "$FAKE_TOOLS/wiregate" <<'WIRE'
#!/usr/bin/env sh
exit 0
WIRE
cat > "$FAKE_TOOLS/curl" <<'CURL'
#!/usr/bin/env sh
set -eu
for arg; do
 if [ "${output_next:-}" = 1 ]; then printf 'asset\n' > "$arg"; exit 0; fi
 if [ "$arg" = -o ]; then output_next=1; fi
 case "$arg" in
  */demos/water) printf '<div data-gosx-scene3d></div><script src="/bootstrap-feature-scene3d.abc.js"></script><script src="/bootstrap-feature-scene3d-webgpu.abc.js"></script>\n' ;;
 esac
done
CURL
chmod 700 "$FAKE_TOOLS"/*
export PATH="$FAKE_TOOLS:$PATH" GO="$FAKE_TOOLS/go"
export GOSX_LISTEN_ADDR="127.0.0.1:invalid"
cd "$tmp_dir"
sh "$script_dir/prod-water-smoke.sh" >/dev/null
WIRE_GATE_REUSE_DOCS=1 sh "$script_dir/wire-gate.sh" >/dev/null
# Readiness is stubbed, so allow the three child launch shims to record binds.
attempt=0
while [ "$(wc -l < "$FAKE_LISTEN_LOG")" -lt 3 ] && [ "$attempt" -lt 20 ]; do
 sleep 0.1
 attempt=$((attempt+1))
done
for addr in 127.0.0.1:8128 127.0.0.1:8742 127.0.0.1:8743; do
 if ! grep -Fx "$addr" "$FAKE_LISTEN_LOG" >/dev/null; then
  echo "local listen overrides test: missing launch at $addr" >&2
  exit 1
 fi
done
echo "local listen overrides test: passed"
