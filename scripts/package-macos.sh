#!/bin/sh
# Builds the universal macOS desktop app and packages it as a DMG:
#
#   scripts/package-macos.sh VERSION COMMIT DATE
#
# Without signing credentials the app is ad-hoc signed and Gatekeeper asks the
# user to confirm the first launch. With APPLE_SIGNING_IDENTITY set, the app is
# signed with the hardened runtime; with APPLE_ID, APPLE_TEAM_ID, and
# APPLE_APP_PASSWORD also set, the DMG is notarized and stapled.
set -eu

version=${1:?version}
commit=${2:?commit}
date=${3:?date}
root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
app="$root/desktop/build/bin/ovm.app"
dmg="$dist/ovm_${version}_macos_universal.dmg"
package=github.com/addodelgrossi/ovm/internal/buildinfo

mkdir -p "$dist"
cd "$root/desktop"
MACOSX_DEPLOYMENT_TARGET=13.0 CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 \
  GOTOOLCHAIN=go1.27.2 go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build -clean \
  -platform darwin/universal -trimpath \
  -ldflags "-s -w -X $package.Version=$version -X $package.Commit=$commit -X $package.Date=$date"

plist="$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $version" "$plist"
lipo -archs "$app/Contents/MacOS/ovm" | grep -q x86_64
lipo -archs "$app/Contents/MacOS/ovm" | grep -q arm64

if [ -n "${APPLE_SIGNING_IDENTITY:-}" ]; then
  codesign --force --deep --options runtime --timestamp --sign "$APPLE_SIGNING_IDENTITY" "$app"
else
  echo "APPLE_SIGNING_IDENTITY is not set; ad-hoc signing" >&2
  codesign --force --deep --sign - "$app"
fi
codesign --verify --deep --strict "$app"

staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
cp -R "$app" "$staging/"
ln -s /Applications "$staging/Applications"
cp "$root/LICENSE" "$root/THIRD_PARTY_NOTICES" "$staging/"
rm -f "$dmg"
hdiutil create -volname "ovm $version" -srcfolder "$staging" -fs HFS+ -format UDZO -ov "$dmg" >/dev/null

if [ -n "${APPLE_SIGNING_IDENTITY:-}" ]; then
  codesign --force --timestamp --sign "$APPLE_SIGNING_IDENTITY" "$dmg"
  if [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ] && [ -n "${APPLE_APP_PASSWORD:-}" ]; then
    xcrun notarytool submit "$dmg" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" \
      --password "$APPLE_APP_PASSWORD" --wait
    xcrun stapler staple "$dmg"
  fi
fi
echo "$dmg"
