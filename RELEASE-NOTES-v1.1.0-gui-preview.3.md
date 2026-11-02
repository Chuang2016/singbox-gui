# sbgui v1.1.0-gui-preview.3 (preview release)

**Preview 3 changes the native front-end technology.** The Fyne
builds in gui-preview.1 / gui-preview.2 are superseded: the Fyne
window used ~189 MB RSS in testing, which conflicts with sbgui's
small-and-portable goal. The Fyne code stays in the tree under
`gui/`, frozen, and is no longer shipped. (A WebView-shell design
was also considered and dropped before implementation.)

The production line remains v1.0.9 (web dashboard).

## Architecture: one exe, two faces

The Windows preview build is a single portable exe containing the
engine (`core/`), the existing web server, and a new native window
built with **Walk (lxn/walk)** — real Win32 controls, no browser
runtime, no Qt/WebView dependency:

- **Native window** (`sbgui-walk-windows-amd64.exe`, 12.8 MB,
  `windowsgui` subsystem) — the daily front-end.
- **Web back-end** — the same process serves the full dashboard at
  `http://127.0.0.1:20123`; open it from the window's toolbar button
  or type the address in a browser at any time.

Both faces share one store and one sing-box process; statistics are
shared, not duplicated (one traffic tracker / Clash poller).

### In the native window

- Node table: active marker, Name / Protocol / Latency / Traffic
  columns (latency from the Clash API, traffic = cumulative
  per-node totals)
- Paste share links to import (multi-line supported)
- **Click a node while connected to switch immediately** — config
  regenerates and sing-box restarts in place; the system proxy
  stays on; if the restart fails the selection rolls back and the
  error stays visible in the status bar
- Connect / Disconnect
- System proxy toggle (cleared automatically on disconnect, if
  sing-box exits by itself, and on quit)
- Routing mode switch: Bypass CN / Global proxy / Global direct
  (applied immediately while running)
- Latency test: selected node, or all nodes
- Subscriptions: list, add, refresh one / all, auto-refresh on the
  configured interval
- Status bar: state, current node, ports, live up/down rates;
  log pane (front-end + sing-box output)
- Tray icon (Show / Open in browser / Quit); closing the window
  hides it to the tray; single instance — relaunching surfaces the
  existing window
- Remembers nothing it should not: settings live in the usual
  `store.json` next to the exe, shared with the web back-end

### Deliberately only in the web back-end

Rules editor and rule-set packs, connections table, speed test,
full subscription management (delete etc.), gateway mode and the
settings page. The native window links to it; nothing is duplicated.

## Fixes since gui-preview.2

- **Black console window while running** (reported on preview.2):
  fixed at the engine level — the sing-box child process is now
  started with a hidden console. First preview release containing
  this fix.
- **Click-to-switch while connected** (preview.2 required
  disconnect → select → reconnect): implemented, see above.

## Platforms

- **Windows**: the Walk exe above. Needs `sing-box.exe` (>= 1.10)
  next to it. No other runtime dependency — native controls only.
- **Linux**: no native window (Walk is Windows-only). The Linux
  package is the server build as before; the browser dashboard is
  the front-end. See `README-LINUX.txt` in the package.

## Verification status

- Cross-compiled on Linux (pure Go; no mingw). Verified at build
  level: PE subsystem = Windows GUI, icon + manifest resources
  linked, whole module (including the frozen Fyne code) still
  builds, `go vet` clean for the new package.
- **Not yet verified on a real Windows machine**: actual memory
  use, widget rendering/DPI, tray behaviour and the add-subscription
  dialog need a first real-machine run — feedback welcome. (For
  scale: the superseded Fyne build measured ~189 MB RSS; this one
  is expected to be far lighter, but that is an expectation, not a
  measurement.)

## Upgrading / notes

- `store.json` (nodes, subscriptions, settings) is format-compatible
  with preview.1/.2 and v1.0.9 — keep it next to the exe.
- The web back-end defaults to 127.0.0.1:20123 (changeable with -port); if
  something already occupies it, the window explains and exits.
