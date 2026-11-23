# sbgui v1.1.0-gui-preview.14 (preview release)

Preview 14 fixes the two problems reported against preview.13
on macOS: reopening the window from the menu bar could freeze
the app, and a sing-box left behind by a force-quit blocked the
next launch. Everything from preview.13 is included. The
production line remains v1.0.9.

## macOS: reopening from the menu bar no longer freezes

In preview.13 the tray menu's actions ran on the AppKit main
thread and synchronously asked Gio for a frame (Show Window) or
a window close (Quit). Gio delivers frames and events through a
blocking rendezvous with that same main thread, and the Show
request fired before the window was actually back on screen —
a frame for a still-hidden window may never complete, which
wedged the whole app until it was force-quit. The once-a-second
refresh kept requesting frames while hidden, too.

The tray bridge is now strictly fire-and-forget:

- Menu actions only record state and hand work to the bridge;
  no Gio window call is made synchronously from a menu action
  any more.
- Nothing requests a frame while the window is hidden. The
  window is invalidated only after the bridge has ordered it
  front again and reports it shown.
- Show Window clicks are deduplicated against the bridge's own
  visibility state, so repeated clicks cannot stack window
  operations.
- Quit closes the window through AppKit's native close, which
  reaches the app as a regular close request — the same path
  as the red close button — instead of Gio's blocking
  perform-action call.

## All platforms: force-quit leftovers no longer block startup

sing-box is a child process; when sbgui is force-quit it is
orphaned and keeps holding the local ports, so the next launch
could not start its own sing-box. The shared core now sweeps
for such leftovers when the app starts and again on every
connect:

- A process counts as a leftover only if its command line is
  exactly the invocation sbgui itself issues — the sing-box
  binary with `run -c <sbgui config path>` — **and** its parent
  process is gone. Matching leftovers are terminated (politely
  first, forcibly if needed) and the action is written to the
  log.
- A sing-box whose owner is still alive (for example a second
  sbgui instance) is left alone, and so is any sing-box the
  user runs with their own config. If a port is held by such a
  process, startup reports the usual error rather than killing
  anything.

## Verification

The leftover sweep is covered by automated tests in the core
package, including an end-to-end test with a real orphaned
process and decoys that must survive. The macOS tray changes
are compiled and packaged by the usual macOS workflow; the
hide/show runtime behaviour itself can only be exercised on a
real Mac.
