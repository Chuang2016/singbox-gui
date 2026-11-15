# sbgui v1.1.0-gui-preview.6 (preview release)

Preview 6 closes the gap between the native Windows window (Walk)
and the web dashboard: everything the dashboard does now lives in
the graphical interface, in three tabs. It also bundles the
privproto v2 client. The production line remains v1.0.9.

## The Walk window is now the full interface

- **Nodes tab**
  - New **Speed** column + **Speed test** button (same Clash-API
    measurement as the dashboard: briefly switches the selector to
    the node, downloads a test file, restores the previous node).
  - **Edit node...** -- the dashboard's editor, natively: quick
    fields (name / server / port / SNI / WS Host / WS Path) patched
    into the full outbound JSON. CDN workflow unchanged: preferred
    IP in Server, domain stays in SNI / Host. Saves are validated
    with `sing-box check` and rolled back when rejected (the logic
    moved into `core.UpdateNode`, now shared by both front-ends).
  - **Delete node** (with confirmation) and **Reset traffic**.
  - **Sorting**: click the Name / Latency / Speed / Traffic column
    headers (latency ascending, speed/traffic descending by
    default; click again to reverse; unmeasured nodes sort last).
  - Live up/down rates next to the node actions, in addition to the
    status bar.
- **Rules tab** (new)
  - Block ads / GFW list / auto-start / gateway-mode toggles, saved
    immediately like the dashboard's quick toggles; subscription
    auto-refresh interval.
  - **My lists** (domains / IPs with per-list outbound) and
    **custom rules** (add / delete, validated), committed by
    "Save rules & apply" with a restart offer when running.
  - **Rule-set packs**: cached-pack info, "Update rule data"
    (force refresh with backup/restore), and manual import --
    paste a download URL or pick a file, for geosite-cn / geoip-cn /
    ads / gfwlist, same core functions as the HTTP handlers.
- **Connections tab** (new)
  - Live connections table (destination / process / node / up /
    down / rule), auto-refreshing while the tab is open, with
    **Kill selected** (new `core.KillConnection`).

The web back-end still runs in-process as a fallback; subscription
delete/rename remains there. Both faces share one store and one
sing-box child, as before.

## privproto v2 client bundled

The Windows zip and the Linux tarball now carry the **privproto v2
client** (QUIC-first `auto` transport with TCP-pool fallback and the
wedged-connection watchdog) instead of the v1.1 client, so
privproto nodes -- Sweden (443) and the UK endpoint
`trustclover.eu.org:5443` alike -- use QUIC out of the box.

## Verified

- Windows cross-build compiles clean (`go vet` clean); the binary
  carries the preview.6 version string.
- Full core test suite passes.
- Linux end-to-end against the shared code paths: node edit to a
  CDN preferred IP lands in config.json and passes
  `sing-box check`; a bogus-type edit is rejected and rolled back;
  proxy start, Clash connections list and connection delete (204)
  all work; the bundled v2 client brings up a privproto-uk tunnel
  (204, exit 51.241.150.127) -- from this sandbox UDP is blocked and
  the local DNS is poisoned, so that check ran over the TCP fallback
  to the server's real IP; QUIC itself was already verified from
  InstaBox and in the user's own 40+ Mbps YouTube 4K test.
- The Walk window itself cannot be run in the build sandbox
  (Windows-only UI); it is verified by compilation and by the
  shared-core tests above, as with previews 1-5.
