sbgui native GUI - Linux amd64 (GUI Preview 2)

Run:
  ./sbgui-gui
  Put the sing-box executable (sing-box, >= 1.10) next to sbgui-gui,
  otherwise Connect will fail with a "sing-box binary" error.

Runtime libraries (Debian/Ubuntu package names):
  Required (linked):  libgl1, libglx0 (libGL/libGLX), libwayland-client0,
                      libx11-6, libffi8, libc6
  Recommended (dlopen-ed by GLFW at runtime on X11):
                      libxcursor1, libxrandr2, libxinerama1, libxi6,
                      libxxf86vm1, libxfixes3
  Install all:  sudo apt-get install -y libgl1 libglx0 libwayland-client0 libx11-6 libffi8 libxcursor1 libxrandr2 libxinerama1 libxi6 libxxf86vm1 libxfixes3

Notes:
  - Works on X11 and Wayland desktops (Fyne/GLFW).
  - No GPU required; Mesa software rendering (llvmpipe) is enough.
  - Preview 2 adds: system proxy toggle (GNOME desktops via gsettings;
    other desktops get an error hint) and the routing-rules editor
    (mode presets, ad/GFW pack toggles, my lists, custom rules).
  - Still web-dashboard only: subscriptions, traffic/connections
    views, delay/speed tests, gateway mode, rule-set pack import.
