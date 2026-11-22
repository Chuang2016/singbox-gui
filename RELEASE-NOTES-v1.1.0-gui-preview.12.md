# sbgui v1.1.0-gui-preview.12 (preview release)

Preview 12 fixes a session-restart race that could kill the
external helper clients (naive / privproto) of a freshly started
session, and makes helper-process deaths visible in the log with
their exit status. Everything from preview.11 (naive IPv4
pinning) and preview.10 is included unchanged. The production
line remains v1.0.9.

## Fixed: switching nodes could kill the new session's helpers

Switching nodes restarts the session (Stop, regenerate, Start).
The old session's sing-box watcher goroutine tears the external
clients down asynchronously, and Stop returns before that
teardown has necessarily run. The teardown used to drain
whatever was in the shared client lists when it executed, so a
slow watcher could kill the NEW session's naive and privproto
clients seconds after they had started, and delete their temp
config files. Symptom: switch to a naive node and nothing
connects; the sing-box log shows `connection refused` dialing
the helper's local SOCKS port, for every helper node at once.

Sessions are now generation-scoped: every Start begins a new
generation, clients are registered under their generation, and
every teardown path (Stop, the sing-box exit watcher, Start's
error paths) only ever touches its own generation's clients.
Temp config files are generation-suffixed as well, so one
session's cleanup cannot delete the file a newer session's
client is using.

## Helper exits are now logged, with the exit status

A naive or privproto client that dies on its own after a
successful start is now reported in the log with the process's
exit status verbatim, e.g. `naive: node ai-naive exited:
signal: killed`, and marked as a node error. Clients killed by
normal teardown stay silent.

In addition, the naive client config now enables naiveproxy's
own logging (it logs nothing by default): its output, including
WARNING/ERROR lines and FATAL `Check failed` lines when it
crashes, is forwarded into the sbgui log.

## A note on the Windows naive.exe crash (0x80000003)

Reports from Windows: naive.exe aborts by itself the moment the
first tunnel is dialed, with exception 0x80000003
(STATUS_BREAKPOINT, a Chromium CHECK failure inside naive.exe;
the faulting module is naive.exe itself). That is an upstream
naiveproxy problem, not something sbgui can fix in its own
code -- but until now it was invisible from sbgui's side. With
preview.12, the log shows both the exit status and naive's own
FATAL line naming the failed check, which is the information
needed to pinpoint it.

The bundled naiveproxy is unchanged at **v154.0.8037.49-5**,
which is the latest upstream release (checked 2026-11-23);
there is currently no newer official build to move to.

## Carried over from preview.11 / preview.10

- Naive nodes pin the server domain to its IPv4 (A) address at
  start via a `host-resolver-rules` entry in the client config,
  so the tunnel no longer black-holes on networks without
  working IPv6. The pin is resolved fresh on every start.
- Per-node external client failures: a helper that fails to
  start takes down only its own node; sing-box starts and the
  remaining nodes work.
- WebSocket/CDN ALPN fix (`h2` dropped for WS/HTTPUpgrade
  transports); `http2://` links import as naive nodes; links
  with `server_port` 0 are rejected at import.

## Packages

Same layout as preview.11: Windows zip (Walk window +
privproto-client + naive), Windows plain zip (window + naive),
Linux tarballs (server build; the browser is the front-end),
macOS dmg via the usual build. sing-box is not bundled: put a
sing-box binary (>= 1.10) next to sbgui.
