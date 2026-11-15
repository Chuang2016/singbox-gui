# walkui — native Windows front-end (lxn/walk)

One executable, two faces, one process:

- **Walk native window** — since preview.6 the full front-end, in
  three tabs. Nodes: node table with latency/speed/traffic columns
  and header-click sorting, share-link import, click-to-switch while
  running, Connect/Disconnect, system-proxy toggle, routing-mode
  switch, latency + speed tests, node edit (CDN quick fields + JSON,
  validated with rollback) and delete, traffic reset, subscription
  list/add/refresh (+ auto-refresh on the settings interval), live
  up/down rates, log pane. Rules: routing toggles, my lists, custom
  rules, rule-set pack refresh and manual import (URL / file).
  Connections: live table with kill.
- **Web back-end** — the existing `server` package, embedded in the
  same process on `http://127.0.0.1:20123`, kept as a fallback;
  subscription delete/rename still lives there.

Both faces share one `core.Store` and one sing-box child process.
Statistics are never forked: latency/traffic come from `core`
(`core/clash.go`, `core/traffic.go`) and the front-end reads the
server's traffic tracker through `server.TrafficTracker()` so there
is exactly one Clash poller per process. Node editing is shared too:
`core.UpdateNode` backs both the GUI dialog and
`PUT /api/nodes/{id}`.

Behaviour notes:

- Single instance via a named mutex; a second launch signals the
  first (named event) to surface its window, then exits.
- Close hides to the tray (NotifyIcon: Show / Open in browser /
  Quit). Quit clears the OS proxy, stops sing-box and saves traffic
  totals. The OS proxy is also auto-cleared when sing-box exits by
  itself (watched on a 1 s tick).
- Click-to-switch while running: set active → regen → restart; the
  mixed port (and thus the OS proxy) is unchanged; on failure the
  selection rolls back and the error stays in the status bar.
- The sing-box child is started through `core`'s quiet command
  helper (hidden console on Windows — the preview.2 black-window
  fix); this binary itself is a `windowsgui`-subsystem exe.

Build (pure Go, no mingw needed):

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -trimpath -ldflags "-s -w -H windowsgui" \
  -o sbgui-walk-windows-amd64.exe ./walkui/
```

The exe icon + manifest live in `rsrc_windows_amd64.syso`, generated
from `icon.ico` + `app.manifest` with
`go run github.com/akavel/rsrc@v0.10.2 -ico icon.ico -manifest app.manifest`.
`icon.png` is the same artwork, embedded for the window/tray icon at
runtime.

Linux has no Walk build — the Linux package stays the server build
with the browser as its front-end.
