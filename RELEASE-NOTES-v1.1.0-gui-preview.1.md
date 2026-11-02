# sbgui v1.1.0 GUI Preview 1 (pre-release)

**This is a skeleton preview of the upcoming native GUI — NOT a feature-complete release.**
The production build is still v1.0.9 (web dashboard). Use this preview only to
try the native window/tray experience.

## What's in

- Native Fyne window + system tray (Show/Quit, close-to-tray) — no browser needed
- Node list (name + protocol) backed by the existing `core` store
- Share-link import (vless / trojan / hysteria2 / ss / vmess / tuic via `core.ParseShareLink`)
- Node selection, Connect / Disconnect wired to the real sing-box subprocess
  manager (`core.Manager`), live process log pane, status bar

## What's NOT in (still web-dashboard only)

- Subscription management and auto-refresh
- Routing modes & custom rules editor, rule-set management
- Per-node traffic totals, live traffic rates / charts
- Connections table, delay (urltest) and speed tests
- System-proxy toggle, gateway mode, settings page

## Assets

- `sbgui-gui-windows-amd64.exe` — Windows amd64, 25.4 MB. Put `sing-box.exe`
  (>= 1.10) next to it before connecting.
- `sbgui-gui-linux-amd64.tar.gz` — Linux amd64, 12.0 MB tarball (25.0 MB binary).
  Runtime libraries are listed in the included `README-LINUX.txt`. Put `sing-box`
  (>= 1.10) next to the binary before connecting.

## Notes

- Platforms: Windows + Linux only; there will be no macOS build of the GUI.
- The GUI reuses the same `store.json`, so it can share a folder with the
  existing sbgui data (don't run both at the same time — one sing-box owner).
