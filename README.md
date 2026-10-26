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

Prebuilt binaries are in [Releases](../../releases) (`sbgui.exe` / `sbgui-darwin-arm64` / `sbgui-darwin-amd64` / `sbgui-linux-amd64`, plus a macOS `.app` bundle).

Rule-set packs (geosite-cn / geoip-cn / ads / gfwlist) auto-download from jsDelivr CDN mirrors and refresh weekly. If downloads fail, the last cached copy keeps working — and you can import a pack manually in the dashboard (paste any working URL, or upload a file you downloaded yourself). Per-node cumulative traffic is shown in the node table (polled from live connections every 250ms, persisted in `traffic.json`; very short connections may be undercounted). Nodes can be sorted by delay / speed / traffic (click the column headers) and edited in place (Edit button): quick fields for server, port, SNI, WS host and path — for CDN-fronted nodes, put a preferred CDN IP in Server and keep SNI/Host on your domain — plus the full outbound JSON; edits are validated with `sing-box check` and rolled back when rejected.

macOS first run, if Gatekeeper complains: `xattr -d com.apple.quarantine sbgui-darwin-arm64`

## Build from source

```bash
./build.sh   # outputs dist/: windows-amd64 / darwin-arm64 / darwin-amd64 / linux-amd64 / linux-arm64
```

On Linux, the one-click system proxy toggle uses GNOME `gsettings`; on other desktops set `http_proxy`/`https_proxy` to `http://127.0.0.1:2080` manually.

## Run in background / start on boot (Linux)

1. Check **Auto-start proxy on launch** in the dashboard (Routing rules section), so the proxy comes up without clicking Start.
2. Put the binary and `sing-box` in `~/sbgui/`, then install the service:
```bash
mkdir -p ~/.config/systemd/user
cp deploy/sbgui.service ~/.config/systemd/user/
# edit WorkingDirectory/ExecStart in it if your folder differs
systemctl --user daemon-reload
systemctl --user enable --now sbgui
loginctl enable-linger $USER   # keep running after logout / start on boot
```
Manage with `systemctl --user status|stop|restart sbgui`; logs via `journalctl --user -u sbgui -f`.
Dashboard stays available at http://127.0.0.1:20123 (`-nobrowser` is already set in the service file).

## Linux server (headless)

1. Copy `sbgui-linux-amd64` and the Linux `sing-box` binary to `/opt/sbgui/`, then:
```bash
sudo useradd -r -s /usr/sbin/nologin sbgui
sudo chown -R sbgui:sbgui /opt/sbgui
sudo cp deploy/sbgui-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sbgui
```
2. In the dashboard turn on **Auto-start proxy on launch** once (nodes are stored in `/opt/sbgui/store.json`).
3. Reach the dashboard safely via SSH tunnel instead of exposing it:
```bash
ssh -L 20123:127.0.0.1:20123 user@your-server
# then open http://127.0.0.1:20123 locally
```
The dashboard binds 127.0.0.1 by default; only use `-listen 0.0.0.0` behind a firewall you trust. Logs: `journalctl -u sbgui -f`.

## Gateway mode (Linux, transparent proxy for the whole LAN)

Turn on **Gateway mode** in the dashboard (Routing rules section). sbgui then, as root:
- enables `net.ipv4.ip_forward`,
- installs iptables TPROXY rules redirecting all forwarded TCP/UDP into sing-box,
- adds a `tproxy` inbound (port 2082) to the generated config.

Other devices on the LAN just set this box as their gateway — no per-device proxy settings needed. Traffic still follows your route rules (CN direct, foreign via proxy, ads blocked, DNS hijacked); packets addressed to the box itself (SSH, dashboard) are untouched.

Requirements: run as **root** (for the server service, set `User=root` in `sbgui-server.service`), and a kernel with `xt_TPROXY` (standard on most distros/VPS kernels; check with `modprobe xt_TPROXY`). IPv4 only for now.

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
