# sbgui v1.1.0 GUI Preview 2 (pre-release)

**Preview of the native GUI — still NOT feature-complete.**
The production build is still v1.0.9 (web dashboard).

## What is new in Preview 2

- **System proxy toggle** — one shared implementation (core.SetSystemProxy)
  now used by both the web dashboard and the native GUI:
  Windows registry (HKCU Internet Settings), Linux GNOME (gsettings).
  In the GUI the toggle can be enabled while connected, points the OS at
  127.0.0.1:<mixed port>, and is cleared automatically on Disconnect,
  if sing-box exits, and before Quit — the OS never keeps pointing at
  a dead port. Non-GNOME Linux desktops get an error hint instead.
- **Routing-rules editor** (Rules... button) — mirrors the web dashboard
  card and its settings model: mode presets (Bypass mainland CN /
  Global proxy / Global direct), Block ads + GFW list toggles, the
  user domain/IP lists with their outbounds, and custom rules
  (domain suffix / IP range -> Proxy/Direct/Block) with add/delete
  and validation. Saving while connected offers a restart to apply,
  like the web build.
- **First-start fix** — a fresh install now downloads every rule-set
  pack the default config needs (CN packs, ads pack, GFW list) before
  starting, like the web build; the first Connect no longer fails on
  missing .srs files. Start errors also stay visible in the status
  bar and log pane instead of being overwritten by the status refresh.

## What was already in Preview 1

- Native Fyne window + system tray (Show/Quit, close-to-tray)
- Node list backed by the existing core store, share-link import,
  node selection, Connect / Disconnect via core.Manager, live log pane

## What is NOT in (still web-dashboard only)

- Subscription management and auto-refresh
- Per-node traffic totals, live traffic rates / charts
- Connections table, delay (urltest) and speed tests
- Rule-set pack refresh / manual import (packs are files, not settings)
- Gateway mode, settings page

## Assets

- `sbgui-gui-windows-amd64.exe` — Windows amd64, 25.5 MB. Put `sing-box.exe`
  (>= 1.10) next to it before connecting.
- `sbgui-gui-linux-amd64.tar.gz` — Linux amd64, 12.0 MB tarball (25.1 MB binary).
  Runtime libraries are listed in the included `README-LINUX.txt`. Put `sing-box`
  (>= 1.10) next to the binary before connecting.

## Verification notes

- Linux: rules editor and first-start flow exercised end-to-end under
  Xvfb with a local sing-box server; gsettings write/clear verified in
  an isolated D-Bus session (mode none -> manual -> none).
- Windows: cross-compiled only, NOT yet run on a real Windows machine;
  the registry code path is the same one the web build ships, moved
  into core unchanged.

## Notes

- Platforms: Windows + Linux only; there will be no macOS build of the GUI.
- The GUI reuses the same `store.json`, so it can share a folder with the
  existing sbgui data (do not run both at the same time — one sing-box owner).
