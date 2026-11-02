# sbgui native GUI — Milestone 1 notes

Goal: validate the toolchain and produce a runnable Fyne-based native GUI
(window + system tray) that reuses `core/` as-is, replacing the browser
dashboard. Platforms in scope: **Windows + Linux only** (no macOS).

## Toolchain (InstaBox build host, Debian 13)

- Go 1.27.1 (preinstalled), module `go 1.24.0`
- Fyne `fyne.io/fyne/v2` **v2.8.2** (via go modules)
- Windows cross-compile: `gcc-mingw-w64-x86-64` (GCC 14-win32)
- Linux build deps: `libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev`
  (Fyne 2.8's GLFW compiles both X11 and Wayland backends — Wayland headers
  are required even for X11-only use)

Build commands (from repo root):

```sh
# Linux
CGO_ENABLED=1 go build -ldflags "-s -w" -o dist/sbgui-gui-linux-amd64 ./gui
# Windows
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
  go build -ldflags "-H=windowsgui -s -w" -o dist/sbgui-gui-windows-amd64.exe ./gui
```

## Core <-> GUI coupling brief

`core/` turned out to be cleanly reusable; no changes were needed:

- **Model**: `core.Node{ID, Tag, Protocol, Outbound map[string]any, Source}`,
  `core.Subscription`, `core.Settings`, all persisted as `core.StoreData`.
- **Store**: `core.OpenStore(workDir)` -> `store.Data` (exported field) +
  `store.Save()`. `core.WorkDir()` = exe dir (portable layout preserved).
- **Engine**: `core.NewManager(workDir)` with `Start() / Stop() / Running() /
  Logs() / Check()`; `Manager.Start()` runs `sing-box check` then launches
  `sing-box run -c <workDir>/config.json`.
- **Pure helpers**: `core.ParseShareLink` / `core.LinkTag` / `core.NewID`,
  `core.BuildConfig(data, workDir)`, `core.FetchSubscription`,
  `core.EnsureRuleSets` and friends.

What lives **only in `server/`** (HTTP layer) and the GUI must re-implement
or lift in later milestones:

- Orchestration glue: `Server.StartProxy` / `regen` (rule-sets -> BuildConfig
  -> Manager.Start) — the PoC re-implements this in ~10 lines in `gui/main.go`.
- Subscription refresh (`refreshSub`), settings application, node CRUD.
- Traffic accounting (`server/traffic.go`: Clash API `/connections` polling),
  delay checks (`clashDelay`, `checkAllDelays`), `speedTest`, connections
  table, system-proxy handling — none of these are in `core/` today.

Recommendation: in milestone 2, move `StartProxy`/`regen`, the traffic
tracker and the delay/speed testers from `server/` into `core/` (or a new
`engine/` package) so web and native front-ends share one implementation.

## PoC scope (gui/main.go, ~220 lines)

Implemented: node list (name + protocol, active marker) fed by the real
store, demo-node seeding via `core.ParseShareLink` when the store is empty,
share-link import (parse -> append -> save), node selection (sets
`ActiveNodeID` + save), Connect/Disconnect wired to
`EnsureRuleSets -> BuildConfig -> Manager.Start/Stop` with errors surfaced
in the status bar, 1s status/log polling (`Manager.Logs()`), system tray
menu (Show/Quit), close-to-tray.

Not implemented (web dashboard still has them): subscriptions UI + auto
refresh, mode/rules editing, per-node traffic totals + live rates, traffic
chart, connections table, delay test / speed test, system-proxy toggle,
gateway mode UI, settings page, app icon/branding.

## Verification

- Linux binary runs under Xvfb (software rendering, `LIBGL_ALWAYS_SOFTWARE=1`)
  and renders the full window; see `gui-screenshot.png` (1280x800).
- Connect path is genuinely wired but cannot complete on the build host:
  no `sing-box` binary sits next to the GUI executable there, so
  `Manager.Start()` reports that error in the status bar — expected.
- Windows `.exe` is cross-compiled only (cannot be executed on the Linux
  build host); first real run must happen on a Windows machine.
- Binary size grows vs the web build (~7 MB -> ~25 MB stripped): CGO +
  GLFW/OpenGL linked in. Acceptable for a desktop app; UPX not applied.

---
