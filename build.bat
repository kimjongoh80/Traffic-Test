@echo off
rem Builds ttest for Windows and macOS (Intel + Apple Silicon) into .\dist
if not exist dist mkdir dist
set GOARCH=amd64
set GOOS=windows& go build -trimpath -ldflags "-s -w" -o dist\ttest-windows-amd64.exe .\cmd\ttest || exit /b 1
set GOOS=darwin& set GOARCH=arm64& go build -trimpath -ldflags "-s -w" -o dist\ttest-macos-arm64 .\cmd\ttest || exit /b 1
set GOOS=darwin& set GOARCH=amd64& go build -trimpath -ldflags "-s -w" -o dist\ttest-macos-amd64 .\cmd\ttest || exit /b 1
set GOOS=& set GOARCH=
echo built into .\dist
