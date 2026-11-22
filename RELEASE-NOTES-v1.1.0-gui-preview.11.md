# sbgui v1.1.0-gui-preview.11 (preview release)

Preview 11 is a focused fix on top of preview.10: naive nodes
whose server is a domain name now force IPv4 resolution of that
domain, so the tunnel no longer black-holes on networks without
working IPv6. Everything in preview.10 (per-node external
client failures, the WebSocket/CDN ALPN fix, the import fixes)
is included unchanged. The production line remains v1.0.9.

## Naive nodes: the server domain is pinned to IPv4

A naive node is served by the standalone naiveproxy client,
whose Chromium network stack resolves the server domain itself.
Given a domain with both A and AAAA records, it prefers IPv6 --
and on a network whose IPv6 is broken or absent (many office
networks), the QUIC/TCP connection is attempted over IPv6 and
simply never completes: the client starts, its local SOCKS port
listens, the node even shows as started, but no traffic ever
reaches the server. The same node works fine from a network
with working IPv6, which made this look like a server or
account problem.

sbgui now pins the domain to IPv4 when it launches the client:

- At start, the node's server domain is resolved and its first
  A record is written into the naive client config as a
  Chromium host-resolver rule (`MAP <domain> <ipv4>`), so the
  client connects to the IPv4 address directly.
- The proxy URL keeps the original domain, so TLS SNI and
  certificate verification are unaffected.
- The pin is resolved fresh on every start (never cached), per
  node, from that node's own server name.
- Nothing changes for nodes whose server is already an IP
  literal, for domains that fail to resolve, and for AAAA-only
  domains: no rule is written and the client launches exactly
  as before.

The log shows the pin when one is applied, e.g.
`naive: node ai-naive pins MAP trustclover.eu.org 51.241.150.127`.

Verified end-to-end: a naive+quic node through the Manager
returns 204 with the MAP rule present in the launched client's
config; a rule pointing at a wrong address provably breaks the
connection, so the rule is really in effect, not decorative.

## Carried over from preview.10

- **Per-node external client failures.** A privproto or naive
  client that fails to start (e.g. its binary is missing) no
  longer aborts the whole start: sing-box runs, the remaining
  nodes work, and only the failed node is marked and logged.
  Reminder: `naive` / `naive.exe` and `privproto-client` must
  sit next to the sbgui executable or in PATH; the packages
  ship them in the right place.
- **WebSocket/CDN ALPN fix.** `h2` is dropped from the TLS
  ALPN list for WebSocket and HTTPUpgrade transports, so a CDN
  edge can no longer negotiate h2 and kill the HTTP/1.1
  WebSocket upgrade.
- **Import fixes.** `http2://` links import as naive (https)
  nodes; links with `server_port` 0 are rejected at import
  instead of creating a node that can only fail.

## Packages

Same layout as preview.10: Windows zip (Walk window +
privproto-client + naive), Windows plain zip (window + naive),
Linux tarballs (server build; the browser is the front-end),
macOS dmg via the usual build. sing-box is not bundled: put a
sing-box binary (>= 1.10) next to sbgui.
