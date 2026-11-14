# sbgui v1.1.0-gui-preview.5 (preview release)

Preview 5 adds a new node type on top of preview.4 (Walk native
front-end). The production line remains v1.0.9 (web dashboard).

## New: privproto nodes

sbgui can now use **privproto** servers (the private tunnel protocol;
server = a privproto-server, client = the standalone privproto-client
binary that exposes a local SOCKS5 port).

- **Import** with a share link, in the Walk window or the web
  dashboard, like any other node:

      privproto://<password>@<host>:<port>#<name>

  The password is URL-escaped in generated links; the raw form is
  accepted on import. privproto lines inside subscription bodies
  import too.
- **How it runs:** connecting starts one privproto-client subprocess
  per privproto node (a temp config file next to store.json, removed
  on stop). sing-box sees the node as a plain SOCKS outbound to that
  client's local port, so everything else -- bypass-cn / global
  presets, custom rules, urltest, live node switching, traffic
  accounting -- works unchanged. The privproto password never enters
  the generated sing-box config.
- **Lifecycle:** the clients are part of the managed session.
  Disconnect, node switch, sing-box exiting on its own, web
  back-end quit and Walk window quit all terminate them; no orphan
  processes, and the system-proxy restore behaves as before.
- **Binary:** sbgui does not bundle privproto-client. Place
  `privproto-client.exe` (Windows) / `privproto-client` next to the
  sbgui executable (or in PATH). If it is missing, connecting a
  privproto node fails with a clear "privproto client binary not
  found" error instead of a broken start.

## Verified

- Windows cross-build (this exe) and the Linux server build compile
  clean; `go build ./...` clean; full core test suite passes,
  including new tests for privproto link parsing/escaping, the
  socks-outbound translation, port hand-off to the Manager and
  binary discovery.
- End-to-end on Linux: node imported via the new link, connected
  through sbgui -- generate_204 returns 204 with the Sweden
  server's exit IP; after disconnect no privproto-client or
  sing-box processes remain and all ports are freed.

## Notes

- As before, sbgui does not bundle sing-box either: a sing-box
  binary (1.10 or newer) sits next to the exe.
- The Fyne code under `gui/` remains frozen and unshipped.
