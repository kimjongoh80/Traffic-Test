#!/usr/bin/env bash
# Builds release packages into dist/.
#   ./scripts/package.sh [windows|macos|all] [version]
# windows (any OS, pure Go): TrafficTest-<ver>-windows-x64.zip, -windows-arm64.zip
# macos   (run on a Mac, needs Xcode command line tools for the app window):
#         TrafficTest-<ver>-macos-apple-silicon.zip, -macos-intel.zip
# Requires Go, zip and, for Windows, go-winres (go install github.com/tc-hib/go-winres@latest).
set -euo pipefail
cd "$(dirname "$0")/.."
TARGET="${1:-all}"
VER="${2:-$(sed -n 's/^var version = "\(.*\)"/\1/p' cmd/ttest/main.go)}"
VER="${VER#v}"
OUT=dist
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$OUT"
LDFLAGS="-s -w -X main.version=$VER"

build_windows() {

# Windows resources (icon + version info) as .syso next to main.go.
WINRES=$(command -v go-winres || echo "$(go env GOPATH)/bin/go-winres")
NUMVER="$(echo "$VER" | sed 's/[^0-9.].*//').0"
(cd packaging && "$WINRES" make --in winres.json --out ../cmd/ttest/rsrc \
  --arch amd64,arm64 --file-version "$NUMVER" --product-version "$NUMVER")

for arch in amd64 arm64; do
  name=x64; [ "$arch" = arm64 ] && name=arm64
  d="$WORK/win-$arch/TrafficTest"
  mkdir -p "$d"
  # App (no console window) and a console copy for command-line use.
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS -H windowsgui" -o "$d/TrafficTest.exe" ./cmd/ttest
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$d/ttest.exe" ./cmd/ttest
  cp packaging/Allow-Firewall.bat packaging/Remove-Firewall.bat packaging/UserGuide.html "$d/"
  rm -f "$OUT/TrafficTest-$VER-windows-$name.zip"
  (cd "$WORK/win-$arch" && zip -qr "$OLDPWD/$OUT/TrafficTest-$VER-windows-$name.zip" TrafficTest)
done
rm -f cmd/ttest/rsrc_windows_*.syso
}

build_macos() {
if [ "$(uname)" != Darwin ]; then
  echo "macOS packages need a Mac (the app window uses Cocoa via cgo); skipping" >&2
  return 0
fi
export MACOSX_DEPLOYMENT_TARGET=11.0

for arch in arm64 amd64; do
  name=apple-silicon; [ "$arch" = amd64 ] && name=intel
  root="$WORK/mac-$arch/TrafficTest"
  app="$root/TrafficTest.app/Contents"
  mkdir -p "$app/MacOS" "$app/Resources"
  carch=arm64; [ "$arch" = amd64 ] && carch=x86_64
  CGO_ENABLED=1 CC="clang -arch $carch" GOOS=darwin GOARCH=$arch \
    go build -trimpath -ldflags "$LDFLAGS" -o "$app/MacOS/TrafficTest" ./cmd/ttest
  sed "s/__VERSION__/$VER/g" packaging/Info.plist > "$app/Info.plist"
  cp packaging/TrafficTest.icns "$app/Resources/"
  printf 'APPL????' > "$app/PkgInfo"
  cp "$app/MacOS/TrafficTest" "$root/ttest"   # command-line copy
  cp packaging/UserGuide.html "$root/"
  chmod +x "$app/MacOS/TrafficTest" "$root/ttest"
  codesign --force --deep -s - "$root/TrafficTest.app"   # ad-hoc signature
  rm -f "$OUT/TrafficTest-$VER-macos-$name.zip"
  (cd "$WORK/mac-$arch" && zip -qry "$OLDPWD/$OUT/TrafficTest-$VER-macos-$name.zip" TrafficTest)
done
}

case "$TARGET" in
  windows) build_windows ;;
  macos) build_macos ;;
  all) build_windows; build_macos ;;
  *) echo "usage: $0 [windows|macos|all] [version]" >&2; exit 2 ;;
esac
ls -la "$OUT"
