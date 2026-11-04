# sbgui v1.1.0-gui-preview.4 (preview release)

Preview 4 is a protocol-fix release on top of preview.3 (Walk native
front-end). The production line remains v1.0.9 (web dashboard).

## Fixes

- **AnyTLS nodes could not be imported at all.** The share-link parser
  in  never registered the  scheme, so AnyTLS links
  failed with "unsupported scheme", and AnyTLS nodes inside Clash-format
  subscriptions were silently dropped. Both paths now parse AnyTLS
  (password / sni / insecure / alpn / fingerprint).
- **REALITY links without  produced a config sing-box refuses to
  start.** The generated outbound lacked the uTLS block, and sing-box
  treats that as fatal for the whole config ("uTLS is required by
  reality client") — every node in the profile failed, not just the
  REALITY one. Panel- and converter-exported REALITY links often omit
  ; the generator now defaults the fingerprint to chrome when a
  REALITY link carries none (links with an explicit  are
  unchanged). The Clash conversion path got the same default.

Both fixes live in , so the web-dashboard build benefits too.
Regression tests cover AnyTLS parsing, REALITY with and without ,
and both Clash paths.

## Verified

- Windows cross-build (this exe) and the Linux server build compile
  clean;  clean; new core tests pass.
- Real traffic from the fixed generator: AnyTLS and REALITY (using a
  link deliberately stripped of ) both pass an end-to-end
  generate_204 check through a local sing-box.

## Notes

- sbgui does not bundle sing-box: place a sing-box binary (1.10 or
  newer, with uTLS support) next to the exe, as before. An outdated
  sing-box.exe will still break REALITY independently of this fix.
- The Fyne code under  remains frozen and unshipped.
