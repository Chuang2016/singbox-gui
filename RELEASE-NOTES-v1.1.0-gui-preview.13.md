# sbgui v1.1.0-gui-preview.13 (preview release)

Preview 13 gives the macOS (Gio) build a menu-bar home: closing
the window no longer quits the app. Everything from preview.12
is included unchanged, and the Windows and Linux builds have no
behaviour changes — their version strings simply follow along.
The production line remains v1.0.9.

## macOS: menu-bar residency

The Gio front-end has no system-tray API, so the Mac build now
installs a small native status item through a dedicated
cgo/AppKit bridge (`gapp/tray_darwin.go` / `tray_darwin.m`). All
AppKit work is dispatched to the main queue owned by Gio's NSApp
run loop.

- **Closing the window hides it.** The close (red button or
  Cmd+W) is vetoed through Gio's `ClosingEvent`, the window is
  ordered out, and the app drops its Dock icon while hidden.
  The sing-box core, helper clients and traffic tracking keep
  running — hiding never stops the proxy.
- **The menu-bar icon** is a monochrome template image (the app
  icon's signal-bars-and-dot mark) that follows the system's
  light/dark menu bar. Its menu shows:
  - a status line — `sbgui — Running · node <tag>` or
    `sbgui — Stopped · node <tag>`, refreshed once a second;
  - **Connect / Disconnect** — starts or stops the core from
    the menu, including while the window is hidden (stopping
    also clears the system proxy, as the window's Disconnect
    does);
  - **Show Window** — restores the window (and the Dock icon)
    with all state intact;
  - **Quit** — the regular quit path: clear the system proxy,
    stop sing-box and the helpers, flush traffic totals.
- **Fallback:** if the status item cannot be created, the app
  behaves exactly as before — closing the window quits.

One verification note: the build is compiled and packaged by
the usual macOS workflow, but the hide/show runtime behaviour
itself can only be exercised on a real Mac.

## Carried over from preview.12 / .11 / .10

- Generation-scoped session teardown: switching nodes can no
  longer kill the new session's naive/privproto helper clients.
- Helper-process exits are logged with their exit status, and
  naiveproxy's own log output is forwarded into the sbgui log.
- Naive nodes pin the server domain to its IPv4 (A) address at
  start via a `host-resolver-rules` entry in the client config.
- Per-node external client failures: a helper that fails to
  start takes down only its own node; sing-box starts and the
  remaining nodes work.
- WebSocket/CDN ALPN fix (`h2` dropped for WS/HTTPUpgrade
  transports); `http2://` links import as naive nodes; links
  with `server_port` 0 are rejected at import.

The bundled naiveproxy is unchanged at **v154.0.8037.49-5**,
the latest upstream release (checked 2026-11-23).

## Packages

Same layout as preview.12: Windows zip (Walk window +
privproto-client + naive), Windows plain zip (window + naive),
Linux tarballs (server build; the browser is the front-end),
macOS dmg via the usual build. sing-box is not bundled: put a
sing-box binary (>= 1.10) next to sbgui.
