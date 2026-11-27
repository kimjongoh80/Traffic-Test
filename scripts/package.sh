#!/usr/bin/env bash
# Builds release packages into dist/:
#   TrafficTest-<ver>-windows-x64.zip, -windows-arm64.zip
#   TrafficTest-<ver>-macos-apple-silicon.zip, -macos-intel.zip
# Requires Go, zip and go-winres (go install github.com/tc-hib/go-winres@latest).
set -euo pipefail
cd "$(dirname "$0")/.."
VER="${1:-$(sed -n 's/^var version = "\(.*\)"/\1/p' cmd/ttest/main.go)}"
VER="${VER#v}"
OUT=dist
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
rm -rf "$OUT" && mkdir -p "$OUT"
LDFLAGS="-s -w -X main.version=$VER"
export CGO_ENABLED=0

# Windows resources (icon + version info) as .syso next to main.go.
WINRES=$(command -v go-winres || echo "$(go env GOPATH)/bin/go-winres")
NUMVER="$(echo "$VER" | sed 's/[^0-9.].*//').0"
(cd packaging && "$WINRES" make --in winres.json --out ../cmd/ttest/rsrc \
  --arch amd64,arm64 --file-version "$NUMVER" --product-version "$NUMVER")

for arch in amd64 arm64; do
  name=x64; [ "$arch" = arm64 ] && name=arm64
  d="$WORK/win-$arch/TrafficTest"
  mkdir -p "$d"
  GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$d/TrafficTest.exe" ./cmd/ttest
  cp packaging/Allow-Firewall.bat packaging/Remove-Firewall.bat packaging/UserGuide.html "$d/"
  (cd "$WORK/win-$arch" && zip -qr "$OLDPWD/$OUT/TrafficTest-$VER-windows-$name.zip" TrafficTest)
done
rm -f cmd/ttest/rsrc_windows_*.syso

for arch in arm64 amd64; do
  name=apple-silicon; [ "$arch" = amd64 ] && name=intel
  root="$WORK/mac-$arch/TrafficTest"
  app="$root/TrafficTest.app/Contents"
  mkdir -p "$app/MacOS" "$app/Resources"
  GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$app/MacOS/TrafficTest" ./cmd/ttest
  sed "s/__VERSION__/$VER/g" packaging/Info.plist > "$app/Info.plist"
  cp packaging/TrafficTest.icns "$app/Resources/"
  printf 'APPL????' > "$app/PkgInfo"
  cp "$app/MacOS/TrafficTest" "$root/ttest"   # command-line copy
  cp packaging/UserGuide.html "$root/"
  chmod +x "$app/MacOS/TrafficTest" "$root/ttest"
  (cd "$WORK/mac-$arch" && zip -qry "$OLDPWD/$OUT/TrafficTest-$VER-macos-$name.zip" TrafficTest)
done

(cd "$OUT" && sha256sum *.zip > SHA256SUMS.txt)
ls -la "$OUT"
