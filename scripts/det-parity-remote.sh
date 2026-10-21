#!/usr/bin/env bash
# Run the deterministic-math parity tests on a remote Apple Silicon Mac.
#
# Usage: PARITY_ARM64_HOST=<ssh-alias> scripts/det-parity-remote.sh
#
# The script cross-compiles the test binaries for darwin/arm64, copies them and
# the testdata they read to /tmp/gosx-fir-parity on the host, runs them at low
# priority on 2 threads, and deletes the folder. It touches nothing else on the
# host. Without PARITY_ARM64_HOST it prints a message and exits 0.
set -euo pipefail

host="${PARITY_ARM64_HOST:-}"
if [ -z "$host" ]; then
	echo "det parity: SKIP remote arm64, set PARITY_ARM64_HOST to an ssh alias of an Apple Silicon Mac"
	exit 0
fi

GO="${GO:-go}"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/build/det-parity/remote"
remote=/tmp/gosx-fir-parity
ssh_opts=(-o BatchMode=yes)

rm -rf "$out"
mkdir -p "$out/det" "$out/det/parity" "$out/physics" "$out/vessel"
(
	cd "$root"
	GOOS=darwin GOARCH=arm64 "$GO" test -c -o "$out/det/det.test" ./det
	GOOS=darwin GOARCH=arm64 "$GO" test -c -o "$out/det/parity/parity.test" ./det/parity
	GOOS=darwin GOARCH=arm64 "$GO" test -c -o "$out/physics/physics.test" ./physics
	GOOS=darwin GOARCH=arm64 "$GO" test -c -o "$out/vessel/vessel.test" ./vessel
)
cp -R "$root/physics/testdata" "$out/physics/testdata"
cp -R "$root/vessel/testdata" "$out/vessel/testdata"

cleanup() { ssh "${ssh_opts[@]}" "$host" "rm -rf $remote" || true; }
trap cleanup EXIT

ssh "${ssh_opts[@]}" "$host" "rm -rf $remote && mkdir -p $remote"
scp -q -r "${ssh_opts[@]}" "$out/det" "$out/physics" "$out/vessel" "$host:$remote/"

run() { # dir binary pattern
	echo "== arm64 on $host: $1/$2 -test.run '$3'"
	ssh "${ssh_opts[@]}" "$host" "cd $remote/$1 && taskpolicy -b env GOMAXPROCS=2 ./$2 -test.v -test.run '$3'"
}
run det det.test '.'
run det/parity parity.test '.'
run physics physics.test 'TestParityCorpus|TestFusedMultiplyAddProbeIsSound'
run vessel vessel.test 'TestParityVoyages|TestParityLayout|TestStateCodecRoundTrip'
echo "det parity: arm64 on $host passed"
