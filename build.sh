#!/usr/bin/env sh
# Builds ttest for Windows and macOS (Intel + Apple Silicon) into ./dist
set -e
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$(sed -n 's/^var version = "\(.*\)"/\1/p' cmd/ttest/main.go)" -o dist/ttest-windows-amd64.exe ./cmd/ttest
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$(sed -n 's/^var version = "\(.*\)"/\1/p' cmd/ttest/main.go)" -o dist/ttest-macos-arm64 ./cmd/ttest
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$(sed -n 's/^var version = "\(.*\)"/\1/p' cmd/ttest/main.go)" -o dist/ttest-macos-amd64 ./cmd/ttest
echo "built into ./dist"
