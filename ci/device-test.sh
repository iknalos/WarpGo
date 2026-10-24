#!/usr/bin/env bash
# Runs inside the emulator job: installs the debug build, grants VPN permission
# so no consent dialog appears, runs TunnelTest, then copies its screenshots,
# test results and engine logs out to shots/.
set -uo pipefail
PKG=com.iknalos.warpgo
RUNNER=$PKG.test/androidx.test.runner.AndroidJUnitRunner
mkdir -p shots

gradle installDebug installDebugAndroidTest --no-daemon || exit 1
adb shell appops set "$PKG" ACTIVATE_VPN allow

# am instrument exits 0 even when a test fails, so read its report
out=$(adb shell am instrument -w "$RUNNER")
echo "$out"

for f in $(adb shell run-as "$PKG" ls files/ci 2>/dev/null | tr -d '\r'); do
  adb exec-out run-as "$PKG" cat "files/ci/$f" > "shots/$f"
done
[ -f shots/probes.txt ] && cat shots/probes.txt

echo "$out" | grep -q "^OK (" || { echo "::error::device tests failed"; exit 1; }
