# sbgui v1.1.0-gui-preview.10 (preview release)

Preview 10 is a bug-fix preview on top of preview.9 (naive
support). It fixes the "everything is down" failure that naive
nodes could cause, a WebSocket/CDN node failure, and two import
problems. The production line remains v1.0.9.

## External client failures no longer take everything down

In preview.9, if a privproto or naive client failed to start --
most commonly because the client binary was not found -- the
whole start was aborted and sing-box never ran, so *every* node
looked dead, including plain sing-box nodes like Hysteria2.

Now the failure is per-node:

- sing-box always starts; the remaining nodes work as usual.
- The failed node is recorded (Manager node errors) and named in
  the log, e.g. `naive: node ai-naive failed: naive client
  binary not found: place naive next to the sbgui executable or
  in PATH`.
- Only that node's own traffic fails (its local SOCKS port has
  nothing listening), which is exactly what its latency test
  shows.

Reminder on placement: the naive client (`naive.exe` / `naive`)
and `privproto-client` must sit next to the sbgui executable or
in PATH. The packages ship them in the right place -- if you
move the sbgui binary, move them with it.

## WebSocket nodes behind a CDN work again

Share links for WS nodes often carry `alpn=h3,h2,http/1.1`.
Passed through verbatim, a CDN edge (Cloudflare) negotiates
**h2**, the TLS handshake succeeds, and the HTTP/1.1 WebSocket
upgrade then dies with EOF -- the node never connects. The
parser now drops `h2` from the TLS ALPN list for WebSocket and
HTTPUpgrade transports, so negotiation lands on http/1.1.
Verified end-to-end with a VLESS+WS node fronted by a
Cloudflare edge IP (ws-cdn2 style): 204 through the generated
config.

## Import fixes

- **`http2://` naive links import.** Some panels (S-UI) export
  naive nodes as `http2://<base64(user:pass@host:port)>...`.
  These are now parsed as naive (https transport) nodes instead
  of being skipped as an unsupported scheme.
- **Port-0 links are rejected at import.** A `server_port` of 0
  in an exported link marks a server-side placeholder (e.g. the
  detour companion inbound listed next to a shadowtls node),
  not a node a client could dial. It is now counted with the
  other unparsable lines instead of creating a node that can
  only ever fail.

## Packages

Same layout as preview.9: Windows zip (Walk window +
privproto-client + naive), Windows plain zip (window + naive),
Linux tarballs (server build; the browser is the front-end),
macOS dmg via the usual build. sing-box is not bundled: put a
sing-box binary (>= 1.10) next to sbgui.
