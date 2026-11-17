// sbgui.app launcher for the macOS GUI build (Fyne front-end).
//
// The bundle's real GUI binary lives in Contents/Resources/bin/ together
// with sing-box, privproto-client and naive. On first launch (and
// whenever the bundle version changes) the launcher copies those files into
// ~/Library/Application Support/sbgui/ and then execs the GUI binary
// from there, so core's portable "data next to the exe" logic lands in
// a writable, update-proof data dir and the helper lookup (workDir)
// finds sing-box and privproto-client next to the GUI binary.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const bundleVersion = "1.1.0-gui-preview.9-gio"

var binNames = []string{"sbgui", "sing-box", "privproto-client", "naive"}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no home dir:", err)
		os.Exit(1)
	}
	dataDir := filepath.Join(home, "Library", "Application Support", "sbgui")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "executable:", err)
		os.Exit(1)
	}
	srcBin := filepath.Join(filepath.Dir(exe), "..", "Resources", "bin")

	marker := filepath.Join(dataDir, ".bundle-version")
	cur, _ := os.ReadFile(marker)
	need := string(cur) != bundleVersion
	if !need {
		for _, name := range binNames {
			if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
				need = true
				break
			}
		}
	}
	if need {
		for _, name := range binNames {
			if err := copyFile(filepath.Join(srcBin, name), filepath.Join(dataDir, name)); err != nil {
				fmt.Fprintln(os.Stderr, "install", name, ":", err)
				os.Exit(1)
			}
		}
		_ = os.WriteFile(marker, []byte(bundleVersion), 0o644)
	}

	target := filepath.Join(dataDir, "sbgui")
	if err := os.Chdir(dataDir); err != nil {
		fmt.Fprintln(os.Stderr, "chdir:", err)
		os.Exit(1)
	}
	if err := syscall.Exec(target, []string{target}, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "exec gui:", err)
		os.Exit(1)
	}
}
