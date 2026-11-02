sbgui - Linux amd64 (server build; on Linux the browser is the front-end)

Run:
  ./sbgui                 (options: -port 20123, -listen 127.0.0.1, -nobrowser)
  Put the sing-box executable (sing-box, >= 1.10) next to sbgui,
  then open http://127.0.0.1:20123 in a browser if it does not open
  by itself.

Platform note:
  The native window front-end in this preview is Windows-only
  (Walk / Win32 controls). On Linux this binary is the same
  dual-purpose server as always: it manages sing-box and serves
  the full web dashboard, which is the front-end here.

Layout:
  store.json, config.json, traffic.json and ruleset/ live next to
  the binary (portable). If that folder is not writable, sbgui
  falls back to the user config dir (~/.config/sbgui).
