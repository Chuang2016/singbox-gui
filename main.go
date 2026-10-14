// sbgui: lightweight sing-box GUI manager.
// Single binary: embedded web dashboard, manages a sing-box child process
// (config auto-generated), with link/subscription import, rule-based routing,
// realtime traffic and connection management.
//
// Usage: ./sbgui [-port 20123]
// On first run, put the platform-matching sing-box executable next to sbgui
// (https://github.com/SagerNet/sing-box/releases, >= 1.10 required).
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/server"
)

// openBrowser launches the dashboard URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func main() {
	port := flag.Int("port", 20123, "dashboard port")
	nobrowser := flag.Bool("nobrowser", false, "do not auto-open the dashboard in browser")
	flag.Parse()

	workDir := core.WorkDir()
	if err := os.MkdirAll(workDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "workdir:", err)
		os.Exit(1)
	}
	store, err := core.OpenStore(workDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
	mgr := core.NewManager(workDir)
	srv := server.New(store, mgr, workDir)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Println("sbgui v1.0.4 - sing-box manager")
	fmt.Printf("  dashboard : http://%s\n", addr)
	fmt.Printf("  work dir  : %s\n", workDir)
	fmt.Println("  (config.json, store.json and ruleset/ live in the work dir)")
	fmt.Println("  Put the sing-box executable in the work dir (or PATH), then press Start on the page.")
	fmt.Println("  Minimize this window if you like; close it to stop sbgui.")

	go func() {
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			fmt.Fprintln(os.Stderr, "http:", err)
			os.Exit(1)
		}
	}()

	// desktop-app feel: open the dashboard automatically once it is up
	if !*nobrowser {
		go func() {
			url := "http://" + addr
			for i := 0; i < 50; i++ {
				time.Sleep(100 * time.Millisecond)
				if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
					c.Close()
					break
				}
			}
			openBrowser(url)
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = mgr.Stop()
	fmt.Println("\nbye")
}
