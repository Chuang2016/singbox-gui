package core

import (
	"fmt"
	"os/exec"
	"runtime"
)

// SetSystemProxy points the OS/desktop proxy settings at 127.0.0.1:port
// (the sing-box mixed inbound) or clears them again. It is the single
// implementation shared by the web dashboard (server, /api/sysproxy) and
// the native GUI.
//
//	windows: HKCU Internet Settings (ProxyEnable / ProxyServer)
//	darwin:  networksetup on the Wi-Fi service
//	linux:   GNOME via gsettings (other desktops get an error hint)
func SetSystemProxy(enable bool, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	switch runtime.GOOS {
	case "windows":
		key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
		if enable {
			if out, err := quietCommand("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			if out, err := quietCommand("reg", "add", key, "/v", "ProxyServer", "/t", "REG_SZ", "/d", addr, "/f").CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		} else {
			if out, err := quietCommand("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		}
		return nil
	case "darwin":
		svc := "Wi-Fi"
		if enable {
			if out, err := quietCommand("networksetup", "-setwebproxy", svc, "127.0.0.1", fmt.Sprint(port)).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			if out, err := quietCommand("networksetup", "-setsecurewebproxy", svc, "127.0.0.1", fmt.Sprint(port)).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			if out, err := quietCommand("networksetup", "-setsocksfirewallproxy", svc, "127.0.0.1", fmt.Sprint(port)).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		} else {
			for _, a := range [][]string{{"-setwebproxystate", svc, "off"}, {"-setsecurewebproxystate", svc, "off"}, {"-setsocksfirewallproxystate", svc, "off"}} {
				if out, err := quietCommand("networksetup", a...).CombinedOutput(); err != nil {
					return fmt.Errorf("%v: %s", err, out)
				}
			}
		}
		return nil
	case "linux":
		// GNOME desktop via gsettings
		if _, err := exec.LookPath("gsettings"); err != nil {
			return fmt.Errorf("gsettings not found (non-GNOME desktop?); set http_proxy/https_proxy manually to http://%s", addr)
		}
		run := func(args ...string) error {
			if out, err := quietCommand("gsettings", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			return nil
		}
		if enable {
			for _, a := range [][]string{
				{"set", "org.gnome.system.proxy", "mode", "'manual'"},
				{"set", "org.gnome.system.proxy.http", "host", "'127.0.0.1'"},
				{"set", "org.gnome.system.proxy.http", "port", fmt.Sprint(port)},
				{"set", "org.gnome.system.proxy.https", "host", "'127.0.0.1'"},
				{"set", "org.gnome.system.proxy.https", "port", fmt.Sprint(port)},
				{"set", "org.gnome.system.proxy.socks", "host", "'127.0.0.1'"},
				{"set", "org.gnome.system.proxy.socks", "port", fmt.Sprint(port)},
				{"set", "org.gnome.system.proxy", "ignore-hosts", "['localhost', '127.0.0.0/8']"},
			} {
				if err := run(a...); err != nil {
					return err
				}
			}
		} else {
			if err := run("set", "org.gnome.system.proxy", "mode", "'none'"); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("system proxy toggle only implemented on windows/darwin/linux; set proxy manually to %s", addr)
	}
}
