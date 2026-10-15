#!/bin/bash
# 交叉编译 Win / macOS 二进制 (纯 Go, 无 CGO)
set -e
cd "$(dirname "$0")"
export PATH="$HOME/go-sdk/go/bin:$PATH"
export CGO_ENABLED=0
mkdir -p dist
echo "== windows/amd64 =="
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/sbgui.exe .
echo "== darwin/arm64 (Apple Silicon) =="
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/sbgui-darwin-arm64 .
echo "== darwin/amd64 (Intel Mac) =="
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/sbgui-darwin-amd64 .
echo "== linux/amd64 =="
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/sbgui-linux-amd64 .
echo "== linux/arm64 =="
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/sbgui-linux-arm64 .
ls -la dist/
