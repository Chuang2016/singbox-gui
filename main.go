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
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/server"
)

func main() {
	port := flag.Int("port", 20123, "dashboard port")
	flag.Parse()

	workDir := core.WorkDir()
	if err := os.MkdirAll(workDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "workdir:", err)
		os.Exit(1)
	}
	store, err := core.OpenStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
	mgr := core.NewManager(workDir)
	srv := server.New(store, mgr, workDir)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("sbgui dashboard: http://%s\n", addr)
	fmt.Printf("work dir: %s\n", workDir)
	fmt.Println("Put the sing-box executable in the work dir (or PATH), then press Start on the page.")

	go func() {
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			fmt.Fprintln(os.Stderr, "http:", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = mgr.Stop()
	fmt.Println("\nbye")
}
