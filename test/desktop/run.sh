#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  test/desktop/run.sh phase-1 [app-dir]
  test/desktop/run.sh phase-4 [app-dir]
  test/desktop/run.sh phase-5 [app-dir]
  test/desktop/run.sh phase-7 [app-dir]

Environment:
  GOSX_DESKTOP_SMOKE_DIR     Windows-mounted output dir, default /mnt/c/temp/gosx-desktop-smoke
  GOSX_DESKTOP_SMOKE_ARCH    Windows GOARCH, default amd64
  GOSX_DESKTOP_SMOKE_LAUNCH  Set to 0 to build only, default 1
USAGE
}

phase="${1:-}"
case "$phase" in
  phase-1 | 1 | phase-4 | 4 | phase-5 | 5 | phase-7 | 7) ;;
  -h | --help | help)
    usage
    exit 0
    ;;
  *)
    usage
    exit 2
    ;;
esac
shift

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
app_dir="${1:-"$repo_root/examples/goetrope-watch"}"
out_dir="${GOSX_DESKTOP_SMOKE_DIR:-/mnt/c/temp/gosx-desktop-smoke}"
arch="${GOSX_DESKTOP_SMOKE_ARCH:-amd64}"
launch="${GOSX_DESKTOP_SMOKE_LAUNCH:-1}"

if [[ ! -d /mnt/c ]]; then
  echo "desktop smoke requires WSL with /mnt/c mounted" >&2
  exit 1
fi
if [[ "$phase" != "phase-5" && "$phase" != "5" && ! -d "$app_dir" ]]; then
  echo "app dir does not exist: $app_dir" >&2
  exit 1
fi

mkdir -p "$out_dir"
exe="$out_dir/gosx-smoke.exe"

(
  cd "$repo_root"
  GOOS=windows GOARCH="$arch" go build -o "$exe" ./cmd/gosx
)

echo "built Windows smoke binary: $exe"
case "$phase" in
  phase-4 | 4)
    cat <<CHECKLIST

Phase 4 manual acceptance:
  1. Desktop window opens against the app dir.
  2. Launch a second copy with the same --app-id and --single-instance.
  3. The second process exits and the first terminal logs the forwarded args.
  4. Deep-link/file-association registry builders are covered by Linux unit tests;
     app-level RegisterProtocol/RegisterFileType calls write per-user HKCU keys on Windows.

CHECKLIST
    ;;
  phase-5 | 5)
    cat <<CHECKLIST

Phase 5 manual acceptance:
  1. Desktop window opens with the GoSX native smoke page.
  2. A tray icon appears with a context menu.
  3. A native notification appears shortly after launch; Notify menu actions fire another one.
  4. The window menu bar and right-click context menu are visible and functional.
  5. Dropping a file onto the window logs file drop paths in the launching terminal.
  6. On a scaled display, the window and hosted content are not blurry.

CHECKLIST
    ;;
  phase-7 | 7)
    cat <<CHECKLIST

Phase 7 manual acceptance:
  1. Build a release package with:
       gosx-smoke.exe build --prod --offline --msix --appinstaller "file:///C:/temp/gosx-desktop-smoke/app.appinstaller" <windows-app-dir>
  2. Confirm dist/app.msix and dist/app.appinstaller are written.
  3. Set GOSX_CODESIGN_CERT and GOSX_CODESIGN_KEY, rerun with --sign, and confirm signtool signs dist/app.msix.
  4. Install the MSIX on the Windows host, publish a second package to the AppInstaller URI, and verify the update prompt/path.

CHECKLIST
    ;;
  *)
    cat <<CHECKLIST

Phase 1 manual acceptance:
  1. Desktop window opens against the app dir.
  2. Press F12. Chromium DevTools opens even without --debug.
  3. Edit a .gsx, .go, css, or js file under the app dir.
  4. The terminal reports rebuild/restart, and the WebView2 window reloads.
  5. No bridge.rate_limited or bridge.too_large errors appear during normal interaction.

CHECKLIST
    ;;
esac

if [[ "$launch" == "0" ]]; then
  exit 0
fi

win_exe="$(wslpath -w "$exe")"
win_app="$(wslpath -w "$app_dir")"
if [[ "$phase" == "phase-4" || "$phase" == "4" ]]; then
  app_id="gosx.desktop.smoke.phase4"
  echo "launching: $win_exe desktop dev --devtools --single-instance --app-id $app_id $win_app"
  cmd.exe /C start "GoSX Desktop Smoke" "$win_exe" desktop dev --devtools --single-instance --app-id "$app_id" "$win_app"
  cat <<SECOND

To verify forwarding after the window is up, run from another shell:
  "$win_exe" desktop --single-instance --app-id "$app_id" --url "gosx-smoke://phase-4?ok=1"

SECOND
elif [[ "$phase" == "phase-5" || "$phase" == "5" ]]; then
  app_id="gosx.desktop.smoke.phase5"
  html='<main style="font:16px Segoe UI,sans-serif;padding:24px"><h1>GoSX native smoke</h1><p>Use the menu bar, tray menu, right-click menu, and drop a file onto this window.</p></main>'
  echo "launching: $win_exe desktop --native-smoke --devtools --app-id $app_id --html ..."
  cmd.exe /C start "GoSX Desktop Smoke" "$win_exe" desktop --native-smoke --devtools --app-id "$app_id" --html "$html"
elif [[ "$phase" == "phase-7" || "$phase" == "7" ]]; then
  cat <<COMMANDS
Release packaging is build-oriented; run this from a Windows shell with Go, MakeAppx, and optional signtool on PATH:

  set GOOS=windows
  set GOARCH=$arch
  "$win_exe" build --prod --offline --msix --appinstaller "file:///C:/temp/gosx-desktop-smoke/app.appinstaller" "$win_app"

For signing, also set GOSX_CODESIGN_CERT and GOSX_CODESIGN_KEY, then add --sign.
COMMANDS
else
  echo "launching: $win_exe desktop dev --devtools $win_app"
  cmd.exe /C start "GoSX Desktop Smoke" "$win_exe" desktop dev --devtools "$win_app"
fi
