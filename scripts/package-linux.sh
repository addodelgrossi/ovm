#!/bin/sh
# Builds the Linux desktop app and the CLI for the host architecture and
# packages both as a Debian package for Ubuntu 22.04 and later:
#
#   scripts/package-linux.sh VERSION COMMIT DATE
#
# Installs /usr/bin/ovm (CLI), /usr/bin/ovm-desktop, a desktop entry, an icon,
# and the license notices.
set -eu

version=${1:?version}
commit=${2:?commit}
date=${3:?date}
root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
package=github.com/addodelgrossi/ovm/internal/buildinfo
ldflags="-s -w -X $package.Version=$version -X $package.Commit=$commit -X $package.Date=$date"

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
# Debian versions cannot contain a leading v or a plus from snapshots.
deb_version=$(printf '%s' "$version" | sed 's/^v//; s/+/~/g')

mkdir -p "$dist"
cd "$root/desktop"
GOTOOLCHAIN=go1.27.2 go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build -clean \
  -platform "linux/$arch" -trimpath -ldflags "$ldflags"
cd "$root"
CGO_ENABLED=0 GOTOOLCHAIN=go1.27.2 go build -trimpath -ldflags "$ldflags" -o "$root/build/ovm-cli-linux-$arch" ./cmd/ovm

staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
install -D -m 0755 "$root/build/ovm-cli-linux-$arch" "$staging/usr/bin/ovm"
install -D -m 0755 "$root/desktop/build/bin/ovm" "$staging/usr/bin/ovm-desktop"
install -D -m 0644 "$root/desktop/build/appicon.png" "$staging/usr/share/icons/hicolor/1024x1024/apps/io.github.addodelgrossi.ovm.png"
install -D -m 0644 "$root/LICENSE" "$staging/usr/share/doc/ovm/copyright"
install -D -m 0644 "$root/THIRD_PARTY_NOTICES" "$staging/usr/share/doc/ovm/THIRD_PARTY_NOTICES"
install -d "$staging/usr/share/applications" "$staging/DEBIAN"
cat > "$staging/usr/share/applications/io.github.addodelgrossi.ovm.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=ovm
Comment=Discover, configure, and diagnose ONVIF-compatible cameras
Exec=/usr/bin/ovm-desktop
Icon=io.github.addodelgrossi.ovm
Categories=Network;Video;Utility;
Terminal=false
DESKTOP
cat > "$staging/DEBIAN/control" <<CONTROL
Package: ovm
Version: $deb_version
Section: net
Priority: optional
Architecture: $arch
Depends: libgtk-3-0, libwebkit2gtk-4.0-37
Maintainer: ovm contributors <https://github.com/addodelgrossi/ovm>
Homepage: https://github.com/addodelgrossi/ovm
Description: ONVIF-compatible camera manager
 Desktop and command-line tool to discover ONVIF-compatible cameras, preview
 and measure RTSP streams, apply verified configuration, run diagnostics, and
 export credential-free reports. No telemetry.
CONTROL
deb="$dist/ovm_${version}_linux_${arch}.deb"
dpkg-deb --root-owner-group --build "$staging" "$deb" >/dev/null
dpkg-deb --info "$deb" >/dev/null
echo "$deb"
