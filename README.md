# sbgui — lightweight sing-box GUI manager

A single Go binary with an embedded web dashboard that manages the sing-box core:
import links/subscriptions, rule-based routing, realtime traffic, connection management.

![platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS-blue) ![sing-box](https://img.shields.io/badge/sing--box-%3E%3D1.10-green)

## Features

- **Import nodes**: paste `ss://` `vmess://` `vless://` `trojan://` `hysteria2://` `tuic://` links, one per line
- **Subscriptions**: base64/v2rayN, sing-box JSON and Clash YAML formats, with auto refresh
- **Rule-based routing**: 3 presets (bypass mainland CN / global proxy / global direct) + custom domain/IP rules.
  CN rule-sets are downloaded by sbgui itself via CDN mirrors and stored locally — sing-box starts with zero network dependency.
- **Traffic management**: realtime up/down chart, active connection list, one-click kill
- **Node switching**: click to switch egress; an `auto` group picks by latency
- **System proxy**: one-click toggle (Windows/macOS)
- **Portable**: all files (config, database, rule-sets, logs) live next to `sbgui` — just drop `sing-box` beside it

## Quick start

1. Download sing-box for your platform from [sing-box releases](https://github.com/SagerNet/sing-box/releases) (≥ 1.10, 1.14.x recommended) and extract `sing-box` (`sing-box.exe` on Windows)
2. Put `sbgui` and `sing-box` in the **same folder**
3. Run `sbgui` (double-click on Windows — no console window; the dashboard opens in your browser automatically)
4. Import links or add a subscription → click **Start** → **Enable system proxy**

Desktop behavior: the dashboard is a page, but sbgui itself runs in the background —
you can minimize or close the page and the proxy keeps working. Reopen http://127.0.0.1:20123
anytime, or use the **Quit** button to exit completely. (Start with `-nobrowser` to skip auto-open.)

Prebuilt binaries are in [Releases](../../releases) (`sbgui.exe` / `sbgui-darwin-arm64` / `sbgui-darwin-amd64`).

macOS first run, if Gatekeeper complains: `xattr -d com.apple.quarantine sbgui-darwin-arm64`

## Build from source

```bash
./build.sh   # outputs dist/: windows-amd64 / darwin-arm64 / darwin-amd64
```

Pure Go, no CGO — cross-compile from Linux directly.

## How it works

- `sbgui` generates `config.json` from your nodes/subscriptions/rules, validates it with `sing-box check`, then runs `sing-box` as a child process
- The dashboard talks to sing-box's Clash API for realtime traffic (`/traffic`) and connections (`/connections`)
- DNS always goes direct via local resolver (no proxy dependency, no DoT timeouts)
- Node/subscription data is stored in `store.json` next to the binary

## iOS

An installable iOS app cannot be built from source alone — it requires a Mac with Xcode and a paid Apple Developer account for signing.
The `core/` package (link parsing, subscription fetching, config generation) is pure logic and reusable via gomobile/Flutter.
For daily use on iPhone, App Store clients (Shadowrocket / Stash / Surge / Loon) cover subscriptions and rule-based routing.

## License

MIT
