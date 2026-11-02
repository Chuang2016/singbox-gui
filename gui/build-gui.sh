#!/bin/bash
# Build the native Fyne GUI (CGO) for Windows + Linux into dist/.
#
# The Windows build MUST carry -H=windowsgui: without it the exe is a
# console-subsystem binary and Windows shows a terminal window for the
# app itself. (The sing-box child process is a console binary too; its
# window is suppressed in code instead -- see core/proc_windows.go.)
set -e
cd "$(dirname "$0")/.."
mkdir -p dist
echo "== windows/amd64 (GUI subsystem) =="
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -trimpath -ldflags="-s -w -H=windowsgui" \
  -o dist/sbgui-gui-windows-amd64.exe ./gui
echo "== linux/amd64 =="
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" \
  -o dist/sbgui-gui-linux-amd64 ./gui
echo "== linux tarball =="
rm -rf dist/.pkg && mkdir -p dist/.pkg
cp dist/sbgui-gui-linux-amd64 dist/.pkg/sbgui-gui
cp gui/README-LINUX.txt dist/.pkg/
tar -C dist/.pkg -czf dist/sbgui-gui-linux-amd64.tar.gz sbgui-gui README-LINUX.txt
rm -rf dist/.pkg
ls -la dist/sbgui-gui-windows-amd64.exe dist/sbgui-gui-linux-amd64 dist/sbgui-gui-linux-amd64.tar.gz
